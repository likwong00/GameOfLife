package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

func worker(world [][]byte, imageHeight, imageWidth, startY, endY int, marked *[]cell, wg *sync.WaitGroup, mutex *sync.Mutex) {
	// Signal this goroutine is finished at the end of the function
	defer wg.Done()

	for y := startY; y < endY; y++ {
		for x := 0; x < imageWidth; x++ {

			AliveCellsAround := 0

			// Check neighbours of original cell
			for i := -1; i < 2; i++ {
				for j := -1; j < 2; j++ {
					// Check for how many alive cells are around the original cell (Ignore the original cell)
					// By adding the height and width and then modding it by them deals with out of bound issues
					if y + i == y && x + j == x {
						continue
					} else if world[((y + i) + imageHeight) % imageHeight][((x + j) + imageWidth) % imageWidth] == 0xFF {
						AliveCellsAround++
					}
				}
			}

			// Cases for alive and dead original cells
			// 'break' isn't needed for Golang switch
			switch world[y][x] {
			case 0xFF: // If cell alive
				if AliveCellsAround < 2 || AliveCellsAround > 3 {
					mutex.Lock()
					*marked = append(*marked, cell{x, y})
					mutex.Unlock()
				}
			case 0x00: // If cell dead
				if AliveCellsAround == 3 {
					mutex.Lock()
					*marked = append(*marked, cell{x, y})
					mutex.Unlock()
				}
			}

		}
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell) {

	// Create the 2D slice to store the world.
	world := make([][]byte, p.imageHeight)
	for i := range world {
		world[i] = make([]byte, p.imageWidth)
	}

	// Request the io goroutine to read in the image with the given filename.
	d.io.command <- ioInput
	d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight)}, "x")

	// The io goroutine sends the requested image byte by byte, in rows.
	for y := 0; y < p.imageHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			val := <-d.io.inputVal
			if val != 0 {
				fmt.Println("Alive cell at", x, y)
				world[y][x] = val
			}
		}
	}

	// Calculate the new state of Game of Life after the given number of turns.
	var marked []cell
	var wg sync.WaitGroup
	var mutex sync.Mutex


    var w [p.threads]chan byte
	for thread := 0; thread < p.threads; thread++ {
        w[thread] = make(chan byte)
	}

	for turns := 0; turns < p.turns; turns++ {
		// Sending parts of world to workers
		wg.Add(p.threads)
		saveY := p.imageHeight / p.threads
		startY := 0
		endY := saveY
		for threads :=  0; threads < p.threads; threads++ {
			go worker(world, p.imageHeight, p.imageWidth, startY, endY, &marked, &wg, &mutex)
			startY = endY
			endY += saveY
		}

		// Wait until all goroutines finish
		// Kill/resurrect those marked then reset contents of marked
		wg.Wait()
		for _, c := range marked {
			world[c.y][c.x] = world[c.y][c.x] ^ 0xFF
		}
		marked = nil
	}

	// Create an empty slice to store coordinates of cells that are still alive after p.turns are done.
	var finalAlive []cell
	// Go through the world and append the cells that are still alive.
	for y := 0; y < p.imageHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			if world[y][x] != 0 {
				finalAlive = append(finalAlive, cell{x: x, y: y})
			}
		}
	}

	// Request the io goroutine to write image with given filename.
	d.io.command <- ioOutput
	d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight), strconv.Itoa(p.turns)}, "x")

	// Send the finished state of the world to writePgmImage function
	d.io.finishedWorld <- world

	// Make sure that the Io has finished any output before exiting.
	d.io.command <- ioCheckIdle
	<-d.io.idle

	// Return the coordinates of cells that are still alive.
	alive <- finalAlive
}
