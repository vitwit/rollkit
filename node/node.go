package node

import (
	"github.com/tendermint/tendermint/rpc/client"
)

type Node interface {
	Start() error
	GetClient() client.Client
}
