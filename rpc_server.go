package powerstrip

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/rpc"
	"sync"

	"github.com/zeroFruit/powerstrip/mux"
)

type RPCServer struct {
	Plugins map[string]Plugin

	Stdout, Stderr io.Reader

	DoneCh chan<- struct{}

	lock sync.Mutex

	logger *log.Logger
}

func (s *RPCServer) Init() error { return nil }

func (s *RPCServer) Config() string { return "" }

func (s *RPCServer) Serve(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Printf("[ERR] plugin: plugin server: %s", err)
			return
		}
		go s.ServeConn(conn)
	}
}

func (s *RPCServer) ServeConn(conn io.ReadWriteCloser) {
	mx, err := mux.Server(conn, nil)
	if err != nil {
		conn.Close()
		log.Printf("[ERR] plugin: error creating yamux server: %s", err)
		return
	}

	control, err := mx.Accept()
	if err != nil {
		mx.Close()
		if err != io.EOF {
			log.Printf("[ERR] plugin: error accepting control connection: %s", err)
		}
		return
	}

	// Connect the stdstreams (in, out, err)
	stdstream := make([]net.Conn, 2)
	for i, _ := range stdstream {
		stdstream[i], err = mx.Accept()
		if err != nil {
			mx.Close()
			log.Printf("[ERR] plugin: accepting stream %d: %s", i, err)
			return
		}
	}

	// Copy std streams out to the proper place
	go copyStream("stdout", stdstream[0], s.Stdout)
	go copyStream("stderr", stdstream[1], s.Stderr)

	// Create the broker and start it up
	broker := newMuxBroker(mx)
	go broker.Run()

	// Use the control connection to build the dispenser and serve the
	// connection.
	server := rpc.NewServer()
	server.RegisterName("Control", &controlServer{
		server: s,
	})
	server.RegisterName("Dispenser", &dispenseServer{
		broker:  broker,
		plugins: s.Plugins,
	})
	server.ServeConn(control)
}

// done is called internally by the control server to trigger the
// doneCh to close which is listened to by the main process to cleanly
// exit.
func (s *RPCServer) done() {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.DoneCh != nil {
		close(s.DoneCh)
		s.DoneCh = nil
	}
}

type controlServer struct {
	server *RPCServer
}

func (c *controlServer) Ping(
	null bool, response *struct{}) error {
	*response = struct{}{}
	return nil
}

func (c *controlServer) Quit(
	null bool, response *struct{}) error {
	// End the server
	c.server.done()

	// Always return true
	*response = struct{}{}

	return nil
}

type dispenseServer struct {
	broker  *MuxBroker
	plugins map[string]Plugin
}

func (d *dispenseServer) Dispense(name string, response *uint32) error {
	// Find the function to create this implementation
	p, ok := d.plugins[name]
	if !ok {
		return fmt.Errorf("unknown plugin type: %s", name)
	}
	// Create the implementation first, so we know if there is an error.
	impl, err := p.Server(d.broker)
	if err != nil {
		// We turn the error into an errors error so that it works across RPC
		return errors.New(err.Error())
	}

	// Reserve an ID for our implementation
	id := d.broker.NextId()
	*response = id

	// Run the rest in a goroutine since it can only happen once this RPC
	// call returns. We wait for a connection for the plugin implementation
	// and serve it.
	go func() {
		conn, err := d.broker.Accept(id)
		if err != nil {
			log.Printf("[ERR] go-plugin: plugin dispense error: %s: %s", name, err)
			return
		}

		serve(conn, "Plugin", impl)
	}()

	return nil
}

func serve(conn io.ReadWriteCloser, name string, v interface{}) {
	server := rpc.NewServer()
	if err := server.RegisterName(name, v); err != nil {
		log.Printf("[ERR] go-plugin: plugin dispense error: %s", err)
		return
	}
	server.ServeConn(conn)
}
