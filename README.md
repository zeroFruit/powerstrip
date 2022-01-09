# powerstrip

This is the fork of the [plugin-go](https://github.com/hashicorp/go-plugin) repository. It simplifies the code and only support `net/rpc` client not gRPC. It also removes all the dependecies so it can be run and tested easily.



## Run example

```bash
go mod tidy
cd example/basic
go build -o ./plugin/greeter ./plugin/greeter_impl.go
go build -o basic .
./basic
```

Output would be:

```
[plugin] 2022/01/09 15:36:26 starting plugin path ./plugin/greeter args [./plugin/greeter]
[plugin] 2022/01/09 15:36:26 plugin started path ./plugin/greeter pid 60575
[plugin] 2022/01/09 15:36:26 waiting for RPC address path ./plugin/greeter
Hello!
[plugin] 2022/01/09 15:36:27 plugin process exited  [path ./plugin/greeter pid 60575]
[plugin] 2022/01/09 15:36:27 plugin exited
```



## Run Unit Test

```bash
go mod tidy
go test
```