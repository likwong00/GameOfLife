package main

import (
	"flag"
	"fmt"
	"time"
)

// golParams provides the details of how to run the Game of Life and which image to load.
type golParams struct {
	turns       int
	threads     int
	imageWidth  int
	imageHeight int
}

// ioCommand allows requesting behaviour from the io (pgm) goroutine.
type ioCommand uint8

// This is a way of creating enums in Go.
// It will evaluate to:
//		ioOutput 	= 0
//		ioInput 	= 1
//		ioCheckIdle = 2
const (
	ioOutput ioCommand = iota
	ioInput
	ioCheckIdle
)

// cell is used as the return type for the testing framework.
type cell struct {
	x, y int
}

// distributorToIo defines all chans that the distributor goroutine will have to communicate with the io goroutine.
// Note the restrictions on chans being send-only or receive-only to prevent bugs.
type distributorToIo struct {
	command chan<- ioCommand
	idle    <-chan bool

	filename  chan<- string
	inputVal  <-chan uint8

	worldState chan<- byte
}

// ioToDistributor defines all chans that the io goroutine will have to communicate with the distributor goroutine.
// Note the restrictions on chans being send-only or receive-only to prevent bugs.
type ioToDistributor struct {
	command <-chan ioCommand
	idle    chan<- bool

	filename  <-chan string
	inputVal  chan<- uint8

	worldState <-chan byte
}

// distributorChans stores all the chans that the distributor goroutine will use.
type distributorChans struct {
	io distributorToIo
}

// ioChans stores all the chans that the io goroutine will use.
type ioChans struct {
	distributor ioToDistributor
}

// gameOfLife is the function called by the testing framework.
// It makes some channels and starts relevant goroutines.
// It places the created channels in the relevant structs.
// It returns an array of alive cells returned by the distributor.
func gameOfLife(p golParams, keyChan <-chan rune) []cell {
	// Default channels from structs
	var dChans distributorChans
	var ioChans ioChans

	ioCommand := make(chan ioCommand)
	dChans.io.command = ioCommand
	ioChans.distributor.command = ioCommand

	ioIdle := make(chan bool)
	dChans.io.idle = ioIdle
	ioChans.distributor.idle = ioIdle

	ioFilename := make(chan string)
	dChans.io.filename = ioFilename
	ioChans.distributor.filename = ioFilename

	inputVal := make(chan uint8)
	dChans.io.inputVal = inputVal
	ioChans.distributor.inputVal = inputVal

	worldState := make(chan byte)
	dChans.io.worldState = worldState
	ioChans.distributor.worldState = worldState

	aliveCells := make(chan []cell)

	// Channels for keyboard commands and ticker
	tick := make([]chan struct{}, p.threads)
	aliveNum := make([]chan int, p.threads)
	//state := make(chan bool)
	//	//pause := make(chan bool)
	//	//quit := make(chan bool)

	// Slice of channels of byte for halo implementation
	aComs := make([]chan byte, p.threads)
	bComs := make([]chan byte, p.threads)

	// Initialise keyboard command and ticker channels
	// Initialise all the channels for communication between workers before calling workers
	for t := 0; t < p.threads; t++ {
		tick[t] = make(chan struct{})
		aliveNum[t] = make(chan int)

		aComs[t] = make(chan byte)
		bComs[t] = make(chan byte)
	}

	// -- GOL --
	// Make a slice of channels to send/receive data
	// Instantiate workers
	c := make([]chan byte, p.threads)
	for t := 0; t < p.threads; t++ {
		// If worker is even, send halos first
		c[t] = make(chan byte)
		go worker(p, c[t],
			tick[t], aliveNum[t],
			(t % 2) == 0, aComs[((t - 1) + p.threads) % p.threads], bComs[(t + 1) % p.threads], aComs[t], bComs[t])
	}

	go distributor(p, dChans, aliveCells, c)
	go pgmIo(p, ioChans)

	// -- Keyboard commands and ticker --
	ticker := time.NewTicker(2 * time.Second)
	if keyChan != nil {
		for {
			select {
			//case k := <-keyChan:
			//	switch unicode.ToLower(k) {
			//	case 's':
			//		state <- true
			//	case 'p':
			//		pause <- true
			//	case 'q':
			//		quit <- true
			//		alive := <-aliveCells
			//		return alive
			//	}

			case <-ticker.C:
				a := 0
				for t := 0; t < p.threads; t++ {
					tick[t] <- struct {}{}
					a += <-aliveNum[t]
				}
				fmt.Println("No. of alive cells: ", a)

			case alive := <-aliveCells:
				return alive
			}
		}
	} else {
		alive := <-aliveCells
		return alive
	}
}

// main is the function called when starting Game of Life with 'make gol'
// Do not edit until Stage 2.
func main() {
	var params golParams
	key := make(chan rune)

	flag.IntVar(
		&params.threads,
		"t",
		8,
		"Specify the number of worker threads to use. Defaults to 8.")

	flag.IntVar(
		&params.imageWidth,
		"w",
		512,
		"Specify the width of the image. Defaults to 512.")

	flag.IntVar(
		&params.imageHeight,
		"h",
		512,
		"Specify the height of the image. Defaults to 512.")

	flag.Parse()

	params.turns = 9999999999999

	startControlServer(params)
	go getKeyboardCommand(key)
	gameOfLife(params, key)
	StopControlServer()
}
