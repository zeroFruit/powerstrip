package powerstrip

import (
	"io"
	"net"
)

type ServerProtocol interface {
	Init() error
	Config() string
	Serve(net.Listener)
}

type ClientProtocol interface {
	io.Closer
	Dispense(string) (interface{}, error)
	Ping() error
}
