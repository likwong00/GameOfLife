package main

import (
	"fmt"
	"strconv"
	"strings"
)

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
	for turns := 0; turns < p.turns; turns++ {
		for y := 0; y < p.imageHeight; y++ {
			for x := 0; x < p.imageWidth; x++ {

				AliveCellsAround := 0

				// Check neighbours of original cell
				for i := -1; i < 2; i++ {
					for j := -1; j < 2; j++ {
						// Check for how many alive cells are around the original cell (Ignore the original cell)
						// By adding the height and width and then modding it by them deals with out of bound issues
						if y + i == y && x + j == x {
							continue
						} else if world[((y + i) + p.imageHeight) % p.imageHeight][((x + j) + p.imageWidth) % p.imageWidth] == 0xFF {
							AliveCellsAround++
						}
					}
				}

				// Cases for alive and dead original cells
				// 'break' isn't needed for Golang switch
				switch world[y][x] {
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
