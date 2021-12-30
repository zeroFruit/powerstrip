package main

import (
	"fmt"
	"net"

	"github.com/zeroFruit/powerstrip/mux"
)

func main() {
	// Accept a TCP connection
	lis, err := net.Listen("tcp", "127.0.0.1:8545")
	if err != nil {
		panic(err)
	}
	conn, err := lis.Accept()
	if err != nil {
		panic(err)
	}

	// Setup server side of yamux
	session, err := mux.Server(conn, nil)
	if err != nil {
		panic(err)
	}

	// Accept a stream
	stream, err := session.Accept()
	if err != nil {
		panic(err)
	}

	// Listen for a message
	buf := make([]byte, 4)
	stream.Read(buf)
	fmt.Println("read>>" + string(buf))
}
