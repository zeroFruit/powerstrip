package powerstrip

import (
	"fmt"
	"io"
	"net"
	"net/rpc"

	"github.com/zeroFruit/powerstrip/mux"
)

type RPCClient struct {
	broker  *MuxBroker
	control *rpc.Client
	plugins map[string]Plugin

	stdout, stderr net.Conn
}

func newRPCClient(c *Client) (*RPCClient, error) {
	conn, err := net.Dial(c.addr.Network(), c.addr.String())
	if err != nil {
		return nil, err
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		// Make sure to set keep alive so that the connection doesn't die
		tcpConn.SetKeepAlive(true)
	}

	result, err := NewRPCClient(conn, c.config.Plugins)
	if err != nil {
		conn.Close()
		return nil, err
	}

	err = result.SyncStreams(c.config.SyncStdout, c.config.SyncStderr)
	if err != nil {
		result.Close()
		return nil, err
	}
	return result, nil
}

func NewRPCClient(conn io.ReadWriteCloser, plugins map[string]Plugin) (*RPCClient, error) {
	mx, err := mux.Client(conn, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}

	control, err := mx.Open()
	if err != nil {
		mx.Close()
		return nil, err
	}

	// Connect stdout, stderr streams
	stdstream := make([]net.Conn, 2)
	for i, _ := range stdstream {
		stdstream[i], err = mx.Open()
		if err != nil {
			mx.Close()
			return nil, err
		}
	}
	broker := newMuxBroker(mx)
	go broker.Run()

	return &RPCClient{
		broker:  broker,
		control: rpc.NewClient(control),
		plugins: plugins,
		stdout:  stdstream[0],
		stderr:  stdstream[1],
	}, nil
}

func (c *RPCClient) SyncStreams(stdout io.Writer, stderr io.Writer) error {
	go copyStream("stdout", stdout, c.stdout)
	go copyStream("stderr", stderr, c.stderr)
	return nil
}

func (c *RPCClient) Close() error {
	var empty struct{}
	returnErr := c.control.Call("Control.Quit", true, &empty)

	if err := c.control.Close(); err != nil {
		return err
	}
	if err := c.stdout.Close(); err != nil {
		return err
	}
	if err := c.stderr.Close(); err != nil {
		return err
	}
	if err := c.broker.Close(); err != nil {
		return err
	}
	return returnErr
}

func (c *RPCClient) Dispense(name string) (interface{}, error) {
	p, ok := c.plugins[name]
	if !ok {
		return nil, fmt.Errorf("unknown plugin type: %s", name)
	}
	var id uint32
	if err := c.control.Call(
		"Dispenser.Dispense", name, &id); err != nil {
		return nil, err
	}

	conn, err := c.broker.Dial(id)
	if err != nil {
		return nil, err
	}

	return p.Client(c.broker, rpc.NewClient(conn))
}

func (c *RPCClient) Ping() error {
	var empty struct{}
	return c.control.Call("Control.Ping", true, &empty)
}
