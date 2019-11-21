package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Read = ioInput, Write = ioOutput
func readOrWritePgm(c ioCommand, p golParams, d distributorChans, world [][]byte, turns int) {
	switch c {
	// Request the io goroutine to read in the image with the given filename.
	case ioInput:
		d.io.command <- ioInput
		d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight)}, "x")

	// Request the io goroutine to write image with given filename.
	case ioOutput:
		d.io.command <- ioOutput
		d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight), strconv.Itoa(turns)}, "x")

		// Send the finished state of the world to writePgmImage function
		d.io.worldState <- world
	}
}

func worldToSourceData(world [][]byte, p golParams, saveY, startY, endY int, c chan<- byte, wg *sync.WaitGroup) {
	defer wg.Done()

	// If thread == 1, just do normal logic; otherwise, do conc logic
	switch p.imageHeight - saveY {
	case 0:
		for y := startY; y < endY; y++ {
			for x := 0; x < p.imageWidth; x++ {
				c <- world[y][x]
			}
		}

	default:
		// Properly give channel the bytes for source slice in worker
		// TODO: Possible improvement of y value using modulus
		switch startY {
		// First chan in slice of chans
		case 0:
			// Send last row of world
			for x := 0; x < p.imageWidth; x++ {
				c <- world[p.imageHeight - 1][x]
			}
			// Send rows normally
			for y := startY; y < endY + 1; y++ {
				for x := 0; x < p.imageWidth; x++ {
					c <- world[y][x]
				}
			}

		// Last chan in slice of chans
		case p.imageHeight - saveY:
			// Send rows normally
			for y := startY - 1; y < endY; y++ {
				for x := 0; x < p.imageWidth; x++ {
					c <- world[y][x]
				}
			}
			// Send first row of world
			for x := 0; x < p.imageWidth; x++ {
				c <- world[0][x]
			}

		default:
			// Send rows normally
			for y := startY - 1; y < endY + 1; y++ {
				for x := 0; x < p.imageWidth; x++ {
					c <- world[y][x]
				}
			}
		}

	}
}

func sourceToWorldData(world [][]byte, p golParams, startY, endY int,c <-chan byte, m *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()

	m.Lock()
	for y := startY; y < endY; y++ {
		for x := 0; x < p.imageWidth; x++ {
			world[y][x] = <-c
		}
	}
	m.Unlock()
}

func worker(p golParams, c chan byte) {
	// Markers of which cells should be killed/resurrected
	var marked []cell

	// Create source slice
	sourceY := p.imageHeight/p.threads + 2
	source := make([][]byte, sourceY)
	for i := range source {
		source[i] = make([]byte, p.imageWidth)
	}

	// Infinite loop to:
	// 1. Receive data from world to source
	// 2. Do GOL logic
	// 3. Send data from source to world
	for {
		// 1. Receive data from world
		for y := 0; y < sourceY; y++ {
			for x := 0; x < p.imageWidth; x++ {
				source[y][x] = <-c
			}
		}

		// 2. GOL logic
		// y values indicate that GOL logic shouldn't happen on the first and last row
		for y := 1; y < sourceY - 1; y++ {
			for x := 0; x < p.imageWidth; x++ {
				AliveCellsAround := 0

				// Check for how many alive cells are around the original cell (Ignore the original cell)
				// Adding the width and then modding it by them deals with out of bound issues
				for i := -1; i < 2; i++ {
					for j := -1; j < 2; j++ {
						if y + i == y && x + j == x {
							continue
						} else if source[y + i][((x + j) + p.imageWidth) % p.imageWidth] == 0xFF {
							AliveCellsAround++
						}
					}
				}

				// Cases for alive and dead original cells
				// 'break' isn't needed for Golang switch
				switch source[y][x] {
				case 0xFF: // If cell alive
					if AliveCellsAround < 2 || AliveCellsAround > 3 {
						marked = append(marked, cell{x, y})
					}
				case 0x00: // If cell dead
					if AliveCellsAround == 3 {
						marked = append(marked, cell{x, y})
					}
				}
			}
		}

		// Kill/resurrect those marked then reset contents of marked
		for _, cell := range marked {
			source[cell.y][cell.x] = source[cell.y][cell.x] ^ 0xFF
		}
		marked = nil

		// 3. Send data from source to world
		// y values indicate that we shouldn't send data on the first and last row
		for y := 1; y < sourceY - 1; y++ {
			for x := 0; x < p.imageWidth; x++ {
				c <- source[y][x]
			}
		}
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell, c []chan byte, state, pause, quit chan bool) {
	// Create the 2D slice to store the world.
	world := make([][]byte, p.imageHeight)
	for i := range world {
		world[i] = make([]byte, p.imageWidth)
	}

	// Read pgm image
	readOrWritePgm(ioInput, p, d, world, p.turns)

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
	// Send data to workers, do gol logic, receive data to world.
	// for loops can be "named". This is used to break out of the loop when we signal to quit
	turns := 0
	loop : for turns < p.turns {
		select {
		case <-state:
			go func() {
				readOrWritePgm(ioOutput, p, d, world, turns)
			}()

		case <-pause:
			go func() {
				readOrWritePgm(ioOutput, p, d, world, turns)
			}()

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				<-pause
				fmt.Println("Continuing")
				wg.Done()
			}()
			wg.Wait()

		case <-quit:
			break loop

		default:
			turns++

			// Initialize values for y values
			saveY := p.imageHeight/p.threads
			startY := 0
			endY := saveY

			var wg sync.WaitGroup
			var m sync.Mutex

			// Send data from world to source
			wg.Add(p.threads)
			for t := 0; t < p.threads; t++ {
				go worldToSourceData(world, p, saveY, startY, endY, c[t], &wg)
				startY = endY
				endY += saveY
			}
			
			// Wait until all workers have a completed source
			wg.Wait()
			startY = 0
			endY = saveY

			// Receive data from source to world
			wg.Add(p.threads)
			for t := 0; t < p.threads; t++ {
				go sourceToWorldData(world, p, startY, endY, c[t], &m, &wg)
				startY = endY
				endY += saveY
			}
			wg.Wait()

			readOrWritePgm(ioOutput, p, d, world, turns)
			d.io.worldState <- world
		}
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

	// Write image
	readOrWritePgm(ioOutput, p, d, world, turns)

	// Make sure that the Io has finished any output before exiting.
	d.io.command <- ioCheckIdle
	<-d.io.idle

	// Return the coordinates of cells that are still alive.
	alive <- finalAlive
}
