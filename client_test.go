package powerstrip

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	proc := helperProcess("mock")
	c := NewClient(&ClientConfig{
		Cmd:     proc,
		Plugins: testPluginMap,
	})
	defer c.Kill()

	// Test that it parses the proper address
	addr, err := c.Start()
	if err != nil {
		t.Fatalf("err should be nil, got %s", err)
	}

	if addr.Network() != "tcp" {
		t.Fatalf("bad: %#v", addr)
	}

	if addr.String() != ":1234" {
		t.Fatalf("bad: %#v", addr)
	}

	// Test that it exits properly if killed
	c.Kill()

	// Test that it knows it is exited
	if !c.Exited() {
		t.Fatal("should say client has exited")
	}

	// this test isn't expected to get a client
	if !c.killed() {
		t.Fatal("Client should have failed")
	}
}

func TestClient_testCleanup(t *testing.T) {
	// Create a temporary dir to store the result file
	td, err := ioutil.TempDir("", "plugin")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	defer os.RemoveAll(td)

	// Create a path that the helper process will write on cleanup
	path := filepath.Join(td, "output")

	// Test the cleanup
	process := helperProcess("cleanup", path)
	c := NewClient(&ClientConfig{
		Cmd:     process,
		Plugins: testPluginMap,
	})

	// Grab the client so the process starts
	if _, err := c.Protocol(); err != nil {
		c.Kill()
		t.Fatalf("err: %s", err)
	}

	// Kill it gracefully
	c.Kill()

	// Test for the file
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func TestClient_testInterface(t *testing.T) {
	proc := helperProcess("test-interface")
	c := NewClient(&ClientConfig{
		Cmd:     proc,
		Plugins: testPluginMap,
	})
	defer c.Kill()

	// Grab the RPC client
	proto, err := c.Protocol()
	if err != nil {
		t.Fatalf("err should be nil, got %s", err)
	}

	// Grab the impl
	raw, err := proto.Dispense("test")
	if err != nil {
		t.Fatalf("err should be nil, got %s", err)
	}

	impl, ok := raw.(testInterface)
	if !ok {
		t.Fatalf("bad: %#v", raw)
	}

	result := impl.Double(21)
	if result != 42 {
		t.Fatalf("bad: %#v", result)
	}
	// Kill it
	c.Kill()

	// Test that it knows it is exited
	if !c.Exited() {
		t.Fatal("should say client has exited")
	}

	if c.killed() {
		t.Fatal("process failed to exit gracefully")
	}
}

func TestClient_Start_timeout(t *testing.T) {
	config := &ClientConfig{
		Cmd:          helperProcess("start-timeout"),
		StartTimeout: 50 * time.Millisecond,
		Plugins:      testPluginMap,
	}

	c := NewClient(config)
	defer c.Kill()

	_, err := c.Start()
	if err == nil {
		t.Fatal("err should not be nil")
	}
}

func TestClient_Stderr(t *testing.T) {
	stderr := new(bytes.Buffer)
	process := helperProcess("stderr")
	c := NewClient(&ClientConfig{
		Cmd:     process,
		Stderr:  stderr,
		Plugins: testPluginMap,
	})
	defer c.Kill()

	if _, err := c.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	for !c.Exited() {
		time.Sleep(10 * time.Millisecond)
	}

	if c.killed() {
		t.Fatal("process failed to exit gracefully")
	}

	if !strings.Contains(stderr.String(), "HELLO\n") {
		t.Fatalf("bad log data: '%s'", stderr.String())
	}

	if !strings.Contains(stderr.String(), "WORLD\n") {
		t.Fatalf("bad log data: '%s'", stderr.String())
	}
}

func TestClient_stdin(t *testing.T) {
	// Overwrite stdin for this test with a temporary file
	tf, err := ioutil.TempFile("", "terraform")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	defer os.Remove(tf.Name())
	defer tf.Close()

	if _, err = tf.WriteString("hello"); err != nil {
		t.Fatalf("error: %s", err)
	}

	if err = tf.Sync(); err != nil {
		t.Fatalf("error: %s", err)
	}

	if _, err = tf.Seek(0, 0); err != nil {
		t.Fatalf("error: %s", err)
	}

	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()
	os.Stdin = tf

	proc := helperProcess("stdin")
	c := NewClient(&ClientConfig{
		Cmd:     proc,
		Plugins: testPluginMap,
	})
	defer c.Kill()

	_, err = c.Start()
	if err != nil {
		t.Fatalf("error: %s", err)
	}

	for {
		if c.Exited() {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	if !proc.ProcessState.Success() {
		t.Fatal("process didn't exit cleanly")
	}
}

func TestClient_ping(t *testing.T) {
	process := helperProcess("test-interface")
	c := NewClient(&ClientConfig{
		Cmd:     process,
		Plugins: testPluginMap,
	})
	defer c.Kill()

	// Get the client
	proto, err := c.Protocol()
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	// Ping, should work
	if err := proto.Ping(); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Kill it
	c.Kill()
	if err := proto.Ping(); err == nil {
		t.Fatal("should error")
	}
}
