package powerstrip

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"os/exec"
	"testing"
	"time"
)

// testInterface is the test interface we use for plugins.
type testInterface interface {
	Double(int) int
	PrintKV(string, interface{})
	Bidirectional() error
	PrintStdio(stdout, stderr []byte)
}

// testInterfaceImpl implements testInterface concretely
type testInterfaceImpl struct {
	logger *log.Logger
}

func (i *testInterfaceImpl) Double(v int) int { return v * 2 }

func (i *testInterfaceImpl) PrintKV(key string, value interface{}) {
	i.logger.Println("PrintKV called", key, value)
}

func (i *testInterfaceImpl) Bidirectional() error {
	return nil
}

func (i *testInterfaceImpl) PrintStdio(stdout, stderr []byte) {
	if len(stdout) > 0 {
		fmt.Fprint(os.Stdout, string(stdout))
		os.Stdout.Sync()
	}

	if len(stderr) > 0 {
		fmt.Fprint(os.Stderr, string(stderr))
		os.Stderr.Sync()
	}
}

// testInterfaceServer is the RPC server for testInterfaceClient
type testInterfaceServer struct {
	Broker *MuxBroker
	Impl   testInterface
}

func (s *testInterfaceServer) Double(arg int, resp *int) error {
	*resp = s.Impl.Double(arg)
	return nil
}

func (s *testInterfaceServer) PrintKV(args map[string]interface{}, _ *struct{}) error {
	s.Impl.PrintKV(args["key"].(string), args["value"])
	return nil
}

// testInterfaceClient implements testInterface to communicate over RPC
type testInterfaceClient struct {
	Client *rpc.Client
}

func (impl *testInterfaceClient) Double(v int) int {
	var resp int
	err := impl.Client.Call("Plugin.Double", v, &resp)
	if err != nil {
		panic(err)
	}

	return resp
}

func (impl *testInterfaceClient) PrintKV(key string, value interface{}) {
	err := impl.Client.Call("Plugin.PrintKV", map[string]interface{}{
		"key":   key,
		"value": value,
	}, &struct{}{})
	if err != nil {
		panic(err)
	}
}

func (impl *testInterfaceClient) Bidirectional() error {
	return nil
}

func (impl *testInterfaceClient) PrintStdio(stdout, stderr []byte) {
	// We don't implement this because we test stream syncing another
	// way (see rpc_client_test.go). We probably should test this way
	// but very few people use the net/rpc protocol nowadays so we didn'
	// put in the effort.
	return
}

// testInterfacePlugin is the implementation of Plugin to create
// RPC client/server implementations for testInterface.
type testInterfacePlugin struct {
	Impl testInterface
}

func (p *testInterfacePlugin) Server(b *MuxBroker) (interface{}, error) {
	return &testInterfaceServer{Impl: p.impl()}, nil
}

func (p *testInterfacePlugin) Client(b *MuxBroker, c *rpc.Client) (interface{}, error) {
	return &testInterfaceClient{Client: c}, nil
}

func (p *testInterfacePlugin) impl() testInterface {
	if p.Impl != nil {
		return p.Impl
	}

	return &testInterfaceImpl{
		logger: log.New(os.Stderr, "", log.LstdFlags),
	}
}

// testPluginMap can be used for tests as a plugin map
var testPluginMap = map[string]Plugin{
	"test": new(testInterfacePlugin),
}

func helperProcess(s ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--"}
	cs = append(cs, s...)
	env := []string{
		"GO_WANT_HELPER_PROCESS=1",
	}
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(env, os.Environ()...)
	return cmd
}

// This is not a real test. This is just a helper process kicked off by
// tests.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}

		args = args[1:]
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No command\n")
		os.Exit(2)
	}

	pluginLogger := log.New(os.Stderr, "test-proc", log.LstdFlags)

	testPlugin := &testInterfaceImpl{
		logger: pluginLogger,
	}

	testPluginMap := map[string]Plugin{
		"test": &testInterfacePlugin{Impl: testPlugin},
	}

	cmd, args := args[0], args[1:]
	switch cmd {
	case "stderr":
		fmt.Printf("tcp|:1234\n")
		os.Stderr.WriteString("HELLO\n")
		os.Stderr.WriteString("WORLD\n")
	case "start-timeout":
		time.Sleep(1 * time.Minute)
		os.Exit(1)
	case "mock":
		fmt.Printf("tcp|:1234\n")
		<-make(chan int)
	case "cleanup":
		// Create a defer to write the file. This tests that we get cleaned
		// up properly versus just calling os.Exit
		path := args[0]
		defer func() {
			err := ioutil.WriteFile(path, []byte("foo"), 0644)
			if err != nil {
				panic(err)
			}
		}()

		Serve(&ServeConfig{
			Plugins: testPluginMap,
		})

		// Exit
		return
	case "test-interface":
		Serve(&ServeConfig{
			Plugins: testPluginMap,
		})

		// Shouldn't reach here but make sure we exit anyways
		os.Exit(0)
	case "stdin":
		fmt.Printf("tcp|:1234\n")
		data := make([]byte, 5)
		if _, err := os.Stdin.Read(data); err != nil {
			log.Printf("stdin read error: %s", err)
			os.Exit(100)
		}

		if string(data) == "hello" {
			os.Exit(0)
		}

		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n", cmd)
		os.Exit(2)
	}
}
