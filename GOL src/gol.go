package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
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

func worker(p golParams, c chan byte, size int, sendFirst bool,
	signalWork, signalFinish, signalComplete, state, pause, tick chan struct{}, aliveNum chan int,
	aboveSend, belowSend chan<- byte, belowReceive, aboveReceive <-chan byte) {
	// Markers of which cells should be killed/resurrected
	var marked []cell

	// Create halos
	hAbove := make([]byte, p.imageWidth)
	hBelow := make([]byte, p.imageWidth)

	// Create source slice
	sourceY := size
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
	loop: for {
		select {
		case <-state:
			for y := 0; y < sourceY; y++ {
				for x := 0; x < p.imageWidth; x++ {
					c <- source[y][x]
				}
			}

		case <-pause:
			<-pause

		case <-signalComplete:
			break loop

		case <-tick:
			a := 0
			for y := 0; y < sourceY; y++ {
				for x := 0; x < p.imageWidth; x++ {
					if source[y][x] != 0 {
						a++
					}
				}
			}
			aliveNum <- a

		case <-signalWork:
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
			signalFinish <- struct {}{}
		}
	}

	// Send data from source to world
	for y := 0; y < sourceY; y++ {
		for x := 0; x < p.imageWidth; x++ {
			c <- source[y][x]
		}
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell, c []chan byte, yChan chan int,
	keyChan <-chan rune, signalWork, signalFinish, signalComplete, state, pause, tick []chan struct{},
	aliveNum []chan int) {

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

	// Receive y parameters for each worker
	yParams := make([]int, p.threads + 1)
	for i := 0; i < p.threads + 1; i++ {
		yParams[i] = <-yChan
	}

	var wgData sync.WaitGroup

	// Send data from world to source
	wgData.Add(p.threads)
	for t := 0; t < p.threads; t++ {
		go worldToSourceData(world, p, yParams[t], yParams[t + 1], c[t], &wgData)
	}

	// Wait until all workers have completed source
	wgData.Wait()

	turns := 0
	ticker := time.NewTicker(2 * time.Second)
	loop: for turns < p.turns {
		select {
		case k := <-keyChan:
			switch unicode.ToLower(k) {
			case 's':
				for i := range state {
					state[i] <- struct{}{}
				}

				wgData.Add(p.threads)
				for t := 0; t < p.threads; t++ {
					go sourceToWorldData(world, p, yParams[t], yParams[t + 1], c[t], &wgData)
				}
				wgData.Wait()

				readOrWritePgm(ioOutput, p, d, world, turns)

			case 'p':
				fmt.Println("Paused at turn ", turns)

				for i := range pause {
					pause[i] <- struct{}{}
				}

				wgData.Add(1)
				go func() {
					loop: for {
						select {
						case k := <-keyChan:
							switch unicode.ToLower(k) {
							case 'p':
								fmt.Println("Continuing...")
								for i := range pause {
									pause[i] <- struct{}{}
								}
								wgData.Done()
								break loop

							default:
							}
						}
					}
				}()
				wgData.Wait()

			case 'q':
				fmt.Println("Quitting...")
				break loop

			default:
			}

		case <-ticker.C:
			a := 0
			for i := range tick {
				tick[i] <- struct {}{}
			}
			for i := range aliveNum {
				a += <-aliveNum[i]
			}
			fmt.Println("No. of alive cells: ", a)

		default:
			for i := range signalWork {
				signalWork[i] <- struct {}{}
			}
			for i := range signalFinish {
				<-signalFinish[i]
			}
			turns++
		}
	}

	for i := range signalComplete {
		signalComplete[i] <- struct {}{}
	}

	// Receive data from source to world
	wgData.Add(p.threads)
	for t := 0; t < p.threads; t++ {
		go sourceToWorldData(world, p, yParams[t], yParams[t + 1], c[t], &wgData)
	}
	wgData.Wait()

	// Write image
	readOrWritePgm(ioOutput, p, d, world, turns)

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
