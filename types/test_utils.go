package types

import (
	"math/rand"
	"time"

	"github.com/tendermint/tendermint/crypto/ed25519"
	tmtypes "github.com/tendermint/tendermint/types"
)

// TODO: accept argument for number of validators / proposer index
func GetRandomValidatorSet() *tmtypes.ValidatorSet {
	valSet, _ := GetRandomValidatorSetWithPrivKey()
	return valSet
}

func GetRandomValidatorSetWithPrivKey() (*tmtypes.ValidatorSet, ed25519.PrivKey) {
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()
	return &tmtypes.ValidatorSet{
		Proposer: &tmtypes.Validator{PubKey: pubKey, Address: pubKey.Address()},
		Validators: []*tmtypes.Validator{
			{PubKey: pubKey, Address: pubKey.Address()},
		},
	}, privKey
}

func GetRandomSignedHeader() (*SignedHeader, error) {
	valSet, privKey := GetRandomValidatorSetWithPrivKey()
	signedHeader := &SignedHeader{
		Header: Header{
			Version: Version{
				Block: InitStateVersion.Consensus.Block,
				App:   InitStateVersion.Consensus.App,
			},

			BaseHeader: BaseHeader{
				ChainID: "test",
				Height:  rand.Uint64(), //nolint:gosec,
				Time:    uint64(time.Now().Unix()),
			},
			LastHeaderHash:  GetRandomBytes(32),
			LastCommitHash:  GetRandomBytes(32),
			DataHash:        GetRandomBytes(32),
			ConsensusHash:   GetRandomBytes(32),
			AppHash:         GetRandomBytes(32),
			LastResultsHash: GetRandomBytes(32),
			ProposerAddress: valSet.Proposer.Address,
			AggregatorsHash: valSet.Hash(),
		},
		Validators: valSet,
	}
	commit, err := getCommit(signedHeader.Header, privKey)
	if err != nil {
		return nil, err
	}
	signedHeader.Commit = *commit
	return signedHeader, nil
}

func GetRandomTx() Tx {
	size := rand.Int()%100 + 100 //nolint:gosec
	return Tx(GetRandomBytes(size))
}

func GetRandomBytes(n int) []byte {
	data := make([]byte, n)
	_, _ = rand.Read(data) //nolint:gosec
	return data
}

func getCommit(header Header, privKey ed25519.PrivKey) (*Commit, error) {
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return nil, err
	}
	sign, err := privKey.Sign(headerBytes)
	if err != nil {
		return nil, err
	}
	return &Commit{
		Signatures: []Signature{sign},
	}, nil
}
