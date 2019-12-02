package main

import (
	"flag"
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

	// Slice of channels for worker and distributor
	signalWork := make([]chan struct{}, p.threads)
	signalFinish := make([]chan struct{}, p.threads)
	signalComplete := make([]chan struct{}, p.threads)

	state := make([]chan struct{}, p.threads)
	pause := make([]chan struct{}, p.threads)

	tick := make([]chan struct{}, p.threads)
	aliveNum := make([]chan int, p.threads)

	// Slice of channels of byte for halo implementation
	aComs := make([]chan byte, p.threads)
	bComs := make([]chan byte, p.threads)

	// Initialise all the channels for communication between workers before calling workers
	for t := 0; t < p.threads; t++ {
		signalWork[t] = make(chan struct{})
		signalFinish[t] = make(chan struct{})
		signalComplete[t] = make(chan struct{})

		state[t] = make(chan struct{})
		pause[t] = make(chan struct{})

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
		go worker(p, c[t], (t % 2) == 0,
			signalWork[t], signalFinish[t], signalComplete[t], state[t], pause[t], tick[t], aliveNum[t],
			aComs[((t - 1) + p.threads) % p.threads], bComs[(t + 1) % p.threads], aComs[t], bComs[t])
	}

	go distributor(p, dChans, aliveCells, c,
		keyChan, signalWork, signalFinish, signalComplete, state, pause, tick, aliveNum)
	go pgmIo(p, ioChans)

	alive := <-aliveCells
	return alive
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
