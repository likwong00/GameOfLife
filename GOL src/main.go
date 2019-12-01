package main

import (
	"flag"
	"sync"
	"time"
	"unicode"
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

// Take multiple inputs and send all outputs one by one
func fanInOutAll(in []chan struct{}, out chan struct{}, amount int)  {
	for  {
		for i := 0; i < amount; i++ {
			out<- <-in[i]
		}
	}
}

// (Numbers) Take multiple inputs and send all outputs one by one
func fanInOutAllNum(in []chan int, out chan int, amount int)  {
	for i := 0; i < amount; i++ {
		out<- <-in[i]
	}
}

// Take multiple inputs and send a single output
func fanInOutOne(in []chan struct{}, out chan struct{}, amount int) {
	for i := 0; i < amount - 1; i++ {
		<-in[i]
	}
	out<- <-in[amount - 1]
}

// Take multiple inputs and send a single output
func fanInOutOneNum(in []chan int, out chan int, amount int) {
	for i := 0; i < amount - 1; i++ {
		<-in[i]
	}
	out<- <-in[amount - 1]
}

// gameOfLife is the function called by the testing framework.
// It makes some channels and starts relevant goroutines.
// It places the created channels in the relevant structs.
// It returns an array of alive cells returned by the distributor.
func gameOfLife(p golParams, keyChan <-chan rune) []cell {
	var wg sync.WaitGroup
	wg.Add(p.threads)

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
	tickToWorkers := make([]chan struct{}, p.threads)
	tickToDistributor := make(chan struct{})

	completeFromWorkers := make([]chan struct{}, p.threads)
	completeToDistributor := make(chan struct{})

	stateToWorkers := make([]chan struct{}, p.threads)
	stateToDistributor := make(chan struct{})

	turnsFromWorkers := make([]chan int, p.threads)
	turnsToDistributor := make(chan int)
	//	//pause := make(chan bool)
	//	//quit := make(chan bool)

	// Slice of channels of byte for halo implementation
	aComs := make([]chan byte, p.threads)
	bComs := make([]chan byte, p.threads)

	// Initialise keyboard command and ticker slice channels
	// Initialise all the channels for communication between workers before calling workers
	for t := 0; t < p.threads; t++ {
		tickToWorkers[t] = make(chan struct{})
		completeFromWorkers[t] = make(chan struct{})
		stateToWorkers[t] = make(chan struct{})
		turnsFromWorkers[t] = make(chan int)

		aComs[t] = make(chan byte)
		bComs[t] = make(chan byte)
	}

	// Fan in channels
	go fanInOutOne(completeFromWorkers, completeToDistributor, p.threads)
	go func() {
		for {
			fanInOutOneNum(turnsFromWorkers, turnsToDistributor, p.threads)
		}
	}()

	// -- GOL --
	// Make a slice of channels to send/receive data
	// Instantiate workers
	c := make([]chan byte, p.threads)
	for t := 0; t < p.threads; t++ {
		// If worker is even, send halos first
		c[t] = make(chan byte)
		go worker(p, c[t],
			tickToWorkers[t], completeFromWorkers[t], stateToWorkers[t], turnsFromWorkers[t],
			(t % 2) == 0, aComs[((t - 1) + p.threads) % p.threads], bComs[(t + 1) % p.threads], aComs[t], bComs[t])
	}

	go distributor(p, dChans, aliveCells, c,
		tickToDistributor, completeToDistributor, stateToDistributor, turnsToDistributor)
	go pgmIo(p, ioChans)

	// -- Keyboard commands and ticker --
	ticker := time.NewTicker(2 * time.Second)
	if keyChan != nil {
		for {
			select {
			case k := <-keyChan:
				switch unicode.ToLower(k) {
				case 's':
					stateToDistributor <- struct{}{}
					for t := 0; t < p.threads; t++ {
						stateToWorkers[t] <- struct{}{}
					}
				//case 'p':
				//	for t := 0; t < p.threads; t++ {
				//		pauseToWorkers[t] <- struct{}{}
				//	}
			//	case 'q':
			//		ticker.Stop()
			//		fmt.Println("Quitting...")
			//		for t := 0; t < p.threads; t++ {
			//			quitToWorkers[t] <- struct{}{}
			//			fmt.Println("wefwefwefewf")
			//		}
			//		alive := <-aliveCells
			//		return alive
				}

			case <-ticker.C:
				tickToDistributor <- struct{}{}
				for t := 0; t < p.threads; t++ {
					tickToWorkers[t] <- struct {}{}
				}

			case alive := <-aliveCells:
				return alive
			}
		}
	}

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

	params.turns = 10000

	startControlServer(params)
	go getKeyboardCommand(key)
	gameOfLife(params, key)
	StopControlServer()
}
