package main

import (
	"fmt"
	"log"
	"os/exec"

	"github.com/zeroFruit/powerstrip"
	"github.com/zeroFruit/powerstrip/example/basic/common"
)

var pluginMap = map[string]powerstrip.Plugin{
	"greeter": &common.GreeterPlugin{},
}

func main() {
	client := powerstrip.NewClient(&powerstrip.ClientConfig{
		Plugins: pluginMap,
		Cmd:     exec.Command("./plugin/greeter"),
	})
	defer client.Kill()

	rpcClient, err := client.Protocol()
	if err != nil {
		log.Fatal(err)
	}

	raw, err := rpcClient.Dispense("greeter")
	if err != nil {
		log.Fatal(err)
	}

	greeter := raw.(common.Greeter)
	fmt.Println(greeter.Greet())
}
