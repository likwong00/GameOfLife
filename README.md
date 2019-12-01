# GameOfLife
Model of Conway's Game of Life done in Go

Start worker threads in gameOfLife func in main.go

Each worker has two slices: source and target:
    source size is p.imageHeight/p.threads + 2
    target size is p.imageWidth
    
Send data from the world to the slices byte by byte. Assign a dedicated channel for
each worker to send the data.

GOL logic

We use the two slices approach to "edit on the go." Using the marked approach,
we may not need two slices.

Send the data from the workers back to the world.

cd ../../mnt/c/Users/JONQUIL/IdeaProjects/GameOfLife/GOL\ src/