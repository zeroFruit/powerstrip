package powerstrip

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Client struct {
	config    *ClientConfig
	exited    bool
	l         sync.Mutex
	addr      net.Addr
	proc      *os.Process
	proto     ClientProtocol
	doneCtx   context.Context
	ctxCancel context.CancelFunc

	clientWg sync.WaitGroup
	stderrWg sync.WaitGroup

	logger *log.Logger

	// procKilled is used for testing only, to flag when the process was
	// forcefully killed.
	procKilled bool
}

type ClientConfig struct {
	Plugins      PluginSet
	Cmd          *exec.Cmd
	StartTimeout time.Duration
	Stderr       io.Writer
	SyncStdout   io.Writer
	SyncStderr   io.Writer
}

func NewClient(config *ClientConfig) *Client {
	if config.StartTimeout == 0 {
		config.StartTimeout = 1 * time.Minute
	}
	if config.Stderr == nil {
		config.Stderr = ioutil.Discard
	}
	if config.SyncStdout == nil {
		config.SyncStdout = ioutil.Discard
	}
	if config.SyncStderr == nil {
		config.SyncStderr = ioutil.Discard
	}

	c := &Client{
		config: config,
		logger: log.New(os.Stderr, "[plugin] ", log.LstdFlags),
	}
	return c
}

func (c *Client) Protocol() (ClientProtocol, error) {
	_, err := c.Start()
	if err != nil {
		return nil, err
	}
	c.l.Lock()
	defer c.l.Unlock()

	if c.proto != nil {
		return c.proto, nil
	}

	c.proto, err = newRPCClient(c)
	if err != nil {
		c.proto = nil
		return nil, err
	}
	return c.proto, nil
}

func (c *Client) Start() (net.Addr, error) {
	c.l.Lock()
	defer c.l.Unlock()

	if c.addr != nil {
		return c.addr, nil
	}

	cmd := c.config.Cmd
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Stdin = os.Stdin

	cmdStdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmdStderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	c.logger.Println("starting plugin", "path", cmd.Path, "args", cmd.Args)
	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	c.proc = cmd.Process
	c.logger.Println("plugin started", "path", cmd.Path, "pid", c.proc.Pid)

	// Make sure the command is properly cleaned up if there is an error
	defer func() {
		r := recover()
		if err != nil || r != nil {
			cmd.Process.Kill()
		}
		if r != nil {
			panic(r)
		}
	}()

	// Create a context for when we kill
	c.doneCtx, c.ctxCancel = context.WithCancel(context.Background())

	// Start goroutine that logs the stderr
	c.clientWg.Add(1)
	c.stderrWg.Add(1)
	// logStderr calls Done()
	go c.logStderr(cmdStderr)

	c.clientWg.Add(1)
	go func() {
		defer c.ctxCancel()
		defer c.clientWg.Done()

		// get the cmd info early, since the process information will be removed
		// in Kill.
		pid := c.proc.Pid
		path := cmd.Path

		// wait to finish reading from stderr since the stderr pipe reader
		// will be closed by the subsequent call to cmd.Wait().
		c.stderrWg.Wait()

		err := cmd.Wait()

		debugMsgArgs := []interface{}{
			"path", path,
			"pid", pid,
		}
		if err != nil {
			debugMsgArgs = append(debugMsgArgs,
				[]interface{}{"error", err.Error()}...)
		}

		c.logger.Println("plugin process exited ", debugMsgArgs)
		os.Stderr.Sync()

		c.l.Lock()
		defer c.l.Unlock()
		c.exited = true
	}()

	linesCh := make(chan string)
	c.clientWg.Add(1)
	go func() {
		defer c.clientWg.Done()
		defer close(linesCh)

		sc := bufio.NewScanner(cmdStdout)
		for sc.Scan() {
			linesCh <- sc.Text()
		}
	}()

	timeout := time.After(c.config.StartTimeout)

	var addr net.Addr

	c.logger.Println("waiting for RPC address", "path", cmd.Path)
	select {
	case <-timeout:
		return nil, errors.New("timeout while waiting for plugin to start")
	case <-c.doneCtx.Done():
		return nil, errors.New("plugin exited before we could connect")
	case line := <-linesCh:
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf(
				"Unrecognized remote plugin message: %s\n\n"+
					"This usually means that the plugin is either invalid or simply\n"+
					"needs to be recompiled to support the latest protocol.", line)
		}

		switch parts[0] {
		case "tcp":
			addr, err = net.ResolveTCPAddr("tcp", parts[1])
		case "unix":
			addr, err = net.ResolveUnixAddr("unix", parts[1])
		default:
			err = fmt.Errorf("Unknown address type: %s", parts[0])
		}

		if err != nil {
			return addr, err
		}
	}

	c.addr = addr
	return addr, nil
}

var stdErrBufferSize = 64 * 1024

func (c *Client) logStderr(r io.Reader) {
	defer c.clientWg.Done()
	defer c.stderrWg.Done()

	logger := log.New(os.Stderr, filepath.Base(c.config.Cmd.Path), log.LstdFlags)

	reader := bufio.NewReaderSize(r, stdErrBufferSize)
	// continuation indicates the previous line was a prefix
	continuation := false

	for {
		line, isPrefix, err := reader.ReadLine()
		switch {
		case err == io.EOF:
			return
		case err != nil:
			logger.Println("reading plugin stderr", "error", err)
			return
		}

		c.config.Stderr.Write(line)

		// The line was longer than our max token size, so it's likely
		// incomplete and won't unmarshal.
		if isPrefix || continuation {
			logger.Println(string(line))

			// if we're finishing a continued line, add the newline back in
			if !isPrefix {
				c.config.Stderr.Write([]byte{'\n'})
			}

			continuation = isPrefix
			continue
		}

		c.config.Stderr.Write([]byte{'\n'})
	}
}

// Exited tells whether the underlying process has exited.
func (c *Client) Exited() bool {
	c.l.Lock()
	defer c.l.Unlock()
	return c.exited
}

// killed is used in tests to check if a process failed to exit gracefully, and
// needed to be killed.
func (c *Client) killed() bool {
	c.l.Lock()
	defer c.l.Unlock()
	return c.procKilled
}

// Kill ends the executing subprocess (if it is running) and perform any cleanup
// tasks necessary such as capturing any remaining logs and so on.
//
// This method blocks until the process successfully exits.
//
// This method can safely be called multiple times.
func (c *Client) Kill() {
	// Grab a lock to read some private fields.
	c.l.Lock()
	proc := c.proc
	addr := c.addr
	c.l.Unlock()

	// If there is no process, there is nothing to kill.
	if proc == nil {
		return
	}

	defer func() {
		// Wait for the all client goroutines to finish.
		c.clientWg.Wait()

		// Make sure there is no reference to the old process after it has been
		// killed.
		c.l.Lock()
		c.proc = nil
		c.l.Unlock()
	}()

	// We need to check for address here. It is possible that the plugin
	// started (process != nil) but has no address (addr == nil) if the
	// plugin failed at startup. If we do have an address, we need to close
	// the plugin net connections.
	graceful := false
	if addr != nil {
		// Close the client to cleanly exit the process.
		proto, err := c.Protocol()
		if err == nil {
			err = proto.Close()

			// If there is no error, then we attempt to wait for a graceful
			// exit. If there was an error, we assume that graceful cleanup
			// won't happen and just force kill.
			graceful = err == nil
			if err != nil {
				// If there was an error just log it. We're going to force
				// kill in a moment anyways.
				c.logger.Println("error closing client during Kill", "err", err)
			}
		} else {
			c.logger.Println("client error ", err.Error())
		}
	}

	// If we're attempting a graceful exit, then we wait for a short period
	// of time to allow that to happen. To wait for this we just wait on the
	// doneCh which would be closed if the process exits.
	if graceful {
		select {
		case <-c.doneCtx.Done():
			c.logger.Println("plugin exited")
			return
		case <-time.After(2 * time.Second):
		}
	}

	// If graceful exiting failed, just kill it
	c.logger.Println("plugin failed to exit gracefully")
	proc.Kill()

	c.l.Lock()
	c.procKilled = true
	c.l.Unlock()
}
