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
	for turns := 0; turns < p.turns; turns++ {
		for y := 0; y < p.imageHeight; y++ {
			for x := 0; x < p.imageWidth; x++ {

				AliveCellsAround := 0

				// Case for alive cell, check the current cell to be alive or not
				if world[y][x] == 0xFF {
                    for i := -1; i < 2; i++ {
                        for j := -1; j < 2; j++ {
                            // Ignore the original cell or
							// Check for how many alive cells are around the original cell
                            if y + i == y && x + j == x {
                                continue
                            } else if world[y + i][x + j] == 0xFF {
								AliveCellsAround++
							}
                        }
                    }
                    if AliveCellsAround < 2 || AliveCellsAround > 3 {
                        world[y][x] = world[y][x] ^ 0xFF
                    }
				} else if world[y][x] == 0x00 {
				    for i := -1; i < 2; i++ {
                        for j := -1; j < 2; j++ {
                            // Ignore the original cell or
							// Check for how many alive cells are around the original cell
                            if y + i == y && x + j == x {
                                continue
                            } else if world[y + i][x + j] == 0xFF {
								AliveCellsAround++
							}
                        }
                    }
                    if AliveCellsAround == 3 {
                        world[y][x] = world[y][x] ^ 0xFF
                    }
				}
			}
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

	// Send the finished state of the world to writePgmImage function
	d.io.finishedWorld <- world

	// Make sure that the Io has finished any output before exiting.
	d.io.command <- ioCheckIdle
	<-d.io.idle

	// Return the coordinates of cells that are still alive.
	alive <- finalAlive
}
