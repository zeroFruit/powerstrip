package main

import (
	"net"

	"github.com/zeroFruit/powerstrip/mux"
)

func main() {
	conn, err := net.Dial("tcp", "127.0.0.1:8545")
	if err != nil {
		panic(err)
	}

	// Setup client side of yamux
	session, err := mux.Client(conn, nil)
	if err != nil {
		panic(err)
	}

	// Open a new stream
	stream, err := session.Open()
	if err != nil {
		panic(err)
	}

	// Stream implements net.Conn
	stream.Write([]byte("ping"))
}
