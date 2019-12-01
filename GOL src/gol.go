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
		d.io.command <- c
		d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight)}, "x")

	// Request the io goroutine to write image with given filename.
	case ioOutput:
		d.io.command <- c
		d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight), strconv.Itoa(turns)}, "x")

		// Send the finished state of the world to writePgmImage function
		// TODO: Change this to non memshare version
		for y := 0; y < p.imageHeight; y++ {
			for x := 0; x < p.imageWidth; x++ {
				d.io.worldState <- world[y][x]
			}
		}
	}
}

func worldToSourceData(world [][]byte, p golParams, startY, endY int, c chan<- byte, wg *sync.WaitGroup) {
	defer wg.Done()

	for y := startY; y < endY; y++ {
		for x := 0; x < p.imageWidth; x++ {
			c <- world[y][x]
		}
	}
}

func sourceToWorldData(world [][]byte, p golParams, startY, endY int, c <-chan byte, wg *sync.WaitGroup) {
	defer wg.Done()

	for y := startY; y < endY; y++ {
		for x := 0; x < p.imageWidth; x++ {
			world[y][x] = <-c
		}
	}
}

func worker(p golParams, c chan byte,
	tick, complete, state chan struct{}, turn chan int,
	sendFirst bool, aboveSend, belowSend chan<- byte, belowReceive, aboveReceive <-chan byte) {
	// Markers of which cells should be killed/resurrected
	var marked []cell

	// Create halos
	hAbove := make([]byte, p.imageWidth)
	hBelow := make([]byte, p.imageWidth)

	// Create source slice
	sourceY := p.imageHeight/p.threads
	source := make([][]byte, sourceY)
	for i := range source {
		source[i] = make([]byte, p.imageWidth)
	}

	// Receive data from world
	for y := 0; y < sourceY; y++ {
		for x := 0; x < p.imageWidth; x++ {
			source[y][x] = <-c
		}
	}

	// Loop to:
	// If sendFirst true, this worker sends first then receives halos later
	// Do GOL logic
	turns := 0
	for turns < p.turns {
		select {
		// Every 2 seconds, send number of cells alive
		case <-tick:
			for y := 0; y < sourceY; y++ {
				for x := 0; x < p.imageWidth; x++ {
					c <- source[y][x]
				}
			}

		case <-state:
			for y := 0; y < sourceY; y++ {
				for x := 0; x < p.imageWidth; x++ {
					c <- source[y][x]
				}
			}
			turn <- turns

		default:
			turns++
			switch sendFirst {
			case true:
				// Send halos to neighbour workers
				for x := 0; x < p.imageWidth; x++  {
					aboveSend <- source[0][x]
					belowSend <- source[sourceY - 1][x]
				}

				// Receive halos from neighbour workers
				for x := 0; x < p.imageWidth; x++  {
					hAbove[x] = <-aboveReceive
					hBelow[x] = <-belowReceive
				}

			case false:
				// Receive halos from neighbour workers
				for x := 0; x < p.imageWidth; x++  {
					hBelow[x] = <-belowReceive
					hAbove[x] = <-aboveReceive
				}

				// Send halos to neighbour workers
				for x := 0; x < p.imageWidth; x++  {
					belowSend <- source[sourceY - 1][x]
					aboveSend <- source[0][x]
				}
			}

			// GOL logic
			for y := 0; y < sourceY; y++ {
				for x := 0; x < p.imageWidth; x++ {
					AliveCellsAround := 0

					// Check for how many alive cells are around the original cell (Ignore the original cell)
					// Adding the width and then modding it by them deals with out of bound issues
					for i := -1; i < 2; i++ {
						for j := -1; j < 2; j++ {
							if y + i == y && x + j == x {
								continue
							} else if y + i < 0  {
								if hAbove[((x + j) + p.imageWidth) % p.imageWidth] == 0xFF {
									AliveCellsAround++
								}
							} else if y + i == sourceY {
								if hBelow[((x + j) + p.imageWidth) % p.imageWidth] == 0xFF {
									AliveCellsAround++
								}
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
		}
	}

	// Send data from source to world
	complete <- struct {}{}
	for y := 0; y < sourceY; y++ {
		for x := 0; x < p.imageWidth; x++ {
			c <- source[y][x]
		}
	}
	turn <- turns
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell, c []chan byte,
	tick, complete, state chan struct{}, turn chan int) {

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

	// Initialize values for y values
	saveY := p.imageHeight/p.threads
	startY := 0
	endY := saveY

	var wgData sync.WaitGroup

	// Send data from world to source
	wgData.Add(p.threads)
	for t := 0; t < p.threads; t++ {
		go worldToSourceData(world, p, startY, endY, c[t], &wgData)
		startY = endY
		endY += saveY
	}

	// Wait until all workers have completed source
	wgData.Wait()
	startY = 0
	endY = saveY

	loop: for {
		select {
		case <-complete:
			break loop

		case <-tick:
			wgData.Add(p.threads)
			for t := 0; t < p.threads; t++ {
				go sourceToWorldData(world, p, startY, endY, c[t], &wgData)
				startY = endY
				endY += saveY
			}
			wgData.Wait()
			startY = 0
			endY = saveY

			a := 0
			for y := 0; y < p.imageHeight; y++ {
				for x := 0; x < p.imageWidth; x++ {
					if world[y][x] != 0 {
						a++
					}
				}
			}
			fmt.Println("No. of alive cells: ", a)

		case <-state:
			wgData.Add(p.threads)
			for t := 0; t < p.threads; t++ {
				go sourceToWorldData(world, p, startY, endY, c[t], &wgData)
				startY = endY
				endY += saveY
			}
			wgData.Wait()
			startY = 0
			endY = saveY

			readOrWritePgm(ioOutput, p, d, world, <-turn)
		}
	}

	// Receive data from source to world
	wgData.Add(p.threads)
	for t := 0; t < p.threads; t++ {
		go sourceToWorldData(world, p, startY, endY, c[t], &wgData)
		startY = endY
		endY += saveY
	}
	wgData.Wait()

	// Write image
	readOrWritePgm(ioOutput, p, d, world, <-turn)

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

	// Make sure that the Io has finished any output before exiting.
	d.io.command <- ioCheckIdle
	<-d.io.idle

	// Return the coordinates of cells that are still alive.
	alive <- finalAlive
}
