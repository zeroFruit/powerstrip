package powerstrip

import "net/rpc"

type Plugin interface {
	Server(*MuxBroker) (interface{}, error)
	Client(*MuxBroker, *rpc.Client) (interface{}, error)
}
