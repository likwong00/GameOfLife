package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Read = ioInput, Write = ioOutput
// TODO: Ask if this is memory sharing and if we need to send byte by byte instead
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
		d.io.worldState <- world
	}
}

func worldToSourceData(world [][]byte, p golParams, saveY, startY, endY int, b chan<- byte, wg *sync.WaitGroup) {
	defer wg.Done()

	// If thread == 1, just do normal logic; otherwise, do conc logic
	switch p.imageHeight - saveY {
	case 0:
		for y := startY; y < endY; y++ {
			for x := 0; x < p.imageWidth; x++ {
				b <- world[y][x]
			}
		}

	// Send rows normally
	default:
		for y := startY - 1; y < endY + 1; y++ {
			for x := 0; x < p.imageWidth; x++ {
				b <- world[(y + p.imageHeight) % p.imageHeight][x]
			}
		}
	}
}

func sourceToWorldData(world [][]byte, startY int, c <-chan cell, wg *sync.WaitGroup) {
	defer wg.Done()

	for y := startY - 1; y < endY; y++ {
	    for x := 0; x < p.imageWidth; x++ {
	        world[y][x] = <- c
	    }
	}
}

func worker(p golParams, b chan byte, c chan cell, l chan int, aboveReceive, belowReceive, aboveSend, belowSend chan byte) {
	// Markers of which cells should be killed/resurrected
	var marked []cell

    var wg sync.WaitGroup

	// Create source slice
	sourceY := p.imageHeight/p.threads
	source := make([][]byte, sourceY)
	for i := range source {
		source[i] = make([]byte, p.imageWidth)
	}

	// Receive data from world
	for i := 0; i < p.threads; i++ {
		for y := 0; y < sourceY; y++ {
			for x := 0; x < p.imageWidth; x++ {
				source[y][x] = <- b
			}
		}
	}

	// Slices for receiving channels for top and bottom edge
	topEdge := make([]byte, p.imageWidth)
	bottomEdge := make([]byte, p.imageWidth)


	// Infinite loop to:
	// 1. Update send channels
	// 2. Get things from receive channels
	// 3. Do GOL logic with receive channels
	// ???? 4. Send data from source to world
	for turns := 0; turns < p.turns; turns++{

		// 1. Update send
		wg.Add(1)
		for x := 0; x < p.imageWidth; x++ {
			aboveSend <- source[0][x]
		}
		for x := 0; x < p.imageWidth; x++ {
			belowSend <- source[sourceY - 1][x]
		}

		// 2. Get receive
		for i := range topEdge {
			topEdge[i] = <- aboveReceive
		}
		for i := range topEdge {
			bottomEdge[i] = <- belowReceive
		}
		wg.Done()
		wg.Wait()

		// 3. GOL logic
		wg.Add(1)
		markedLength := 0
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
							if topEdge[((x + j) + p.imageWidth) % p.imageWidth] == 0xFF {
								AliveCellsAround++
							}
						} else if y + i > (sourceY - 1) {
							if bottomEdge[((x + j) + p.imageWidth) % p.imageWidth] == 0xFF {
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
						markedLength++
					}
				case 0x00: // If cell dead
					if AliveCellsAround == 3 {
						marked = append(marked, cell{x, y})
						markedLength++
					}
				}
			}
		}
		// Kill/resurrect those marked then reset contents of marked
		l <- markedLength
		for _, cell := range marked {
			source[cell.y][cell.x] = source[cell.y][cell.x] ^ 0xFF
		}
		marked = nil
		wg.Done()
        wg.Wait()
	}
	// Sending source to distributor after going through all the turns
	for y := 0; y < sourceY; y++ {
	    for x := 0; x < p.imageWidth; x++ {
	        c <- source[y][x]
	    }
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell, b []chan byte, c []chan cell, l []chan int, state, pause, quit chan bool) {
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

    var wg sync.WaitGroup

    // Initialize values for y values
    saveY := p.imageHeight/p.threads
    startY := 0
    endY := saveY

    // Send data from world to source
    for t := 0; t < p.threads; t++ {
    	go worldToSourceData(world, p, saveY, startY, endY, b[t], &wg)
    	startY = endY
    	endY += saveY
    }

	// Calculate the new state of Game of Life after the given number of turns.
	// Send data to workers, do gol logic, receive data to world.
	// for loops can be "named". This is used to break out of the loop when we signal to quit
	turns := 0
	timer := time.After(2 * time.Second)
	loop : for turns < p.turns {
		select {
		// Timer for every 2 seconds
		case <-timer:
			currentAlive := 0
			for y := 0; y < p.imageHeight; y++ {
				for x := 0; x < p.imageWidth; x++ {
					if world[y][x] != 0 {
						currentAlive++
					}
				}
			}

			fmt.Println("No. of cells alive: ", currentAlive)
			timer = time.After(2 * time.Second)

		case <-state:
			go readOrWritePgm(ioOutput, p, d, world, turns)

		case <-pause:
			go readOrWritePgm(ioOutput, p, d, world, turns)

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
		}
	}

	// Receive data from source to world
	wg.Add(p.threads)
	for t := 0; t < p.threads; t++ {
		go sourceToWorldData(world, length, startY - 1, c[t], &wg)
	}
	wg.Wait()

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
