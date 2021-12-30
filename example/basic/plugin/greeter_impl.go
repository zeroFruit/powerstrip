package main

import (
	"github.com/zeroFruit/powerstrip"
	"github.com/zeroFruit/powerstrip/example/basic/common"
)

type GreeterHello struct{}

func (g *GreeterHello) Greet() string {
	return "Hello!"
}

func main() {
	greeter := &GreeterHello{}

	var pluginMap = map[string]powerstrip.Plugin{
		"greeter": &common.GreeterPlugin{Impl: greeter},
	}
	powerstrip.Serve(&powerstrip.ServeConfig{
		Plugins: pluginMap,
	})
}
