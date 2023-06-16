package json

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/proxy"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/rollkit/rollkit/config"
	"github.com/rollkit/rollkit/conv"
	"github.com/rollkit/rollkit/mocks"
	"github.com/rollkit/rollkit/node"
)

// copied from rpc
func getRPC(t *testing.T) (*mocks.Application, rpcclient.Client) {
	t.Helper()
	require := require.New(t)
	app := &mocks.Application{}
	app.On("InitChain", context.Background(), mock.Anything).Return(&abci.ResponseInitChain{}, nil)
	app.On("BeginBlock", context.Background(), mock.Anything).Return(&abci.ResponseBeginBlock{}, nil)
	app.On("EndBlock", context.Background(), mock.Anything).Return(&abci.ResponseEndBlock{}, nil)
	app.On("Commit", context.Background(), mock.Anything).Return(&abci.ResponseCommit{}, nil)
	app.On("GetAppHash", context.Background(), mock.Anything).Return(&abci.ResponseGetAppHash{}, nil)
	app.On("GenerateFraudProof", context.Background(), mock.Anything).Return(&abci.ResponseGenerateFraudProof{}, nil)
	app.On("CheckTx", context.Background(), mock.Anything).Return(&abci.ResponseCheckTx{
		GasWanted: 1000,
		GasUsed:   1000,
	}, nil)
	app.On("Info", mock.Anything).Return(abci.ResponseInfo{
		Data:             "mock",
		Version:          "mock",
		AppVersion:       123,
		LastBlockHeight:  345,
		LastBlockAppHash: nil,
	})
	key, _, _ := crypto.GenerateEd25519Key(rand.Reader)
	validatorKey := ed25519.GenPrivKey()
	nodeKey := &p2p.NodeKey{
		PrivKey: validatorKey,
	}
	signingKey, _ := conv.GetNodeKey(nodeKey)
	pubKey := validatorKey.PubKey()

	genesisValidators := []tmtypes.GenesisValidator{
		{Address: pubKey.Address(), PubKey: pubKey, Power: int64(100), Name: "gen #1"},
	}
	n, err := node.NewNode(context.Background(), config.NodeConfig{Aggregator: true, DALayer: "mock", BlockManagerConfig: config.BlockManagerConfig{BlockTime: 1 * time.Second}, Light: false}, key, signingKey, proxy.NewLocalClientCreator(app), &tmtypes.GenesisDoc{ChainID: "test", Validators: genesisValidators}, log.TestingLogger())
	require.NoError(err)
	require.NotNil(n)

	err = n.Start()
	require.NoError(err)

	local := n.GetClient()
	require.NotNil(local)

	return app, local
}
