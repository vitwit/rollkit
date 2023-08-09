package datasubmit

import (
	//"errors"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// submit data submits the extrinsic through substrate api
func SubmitData(apiURL string, seed string, appID int, data []byte) (types.Hash, error) {
	api, err := gsrpc.NewSubstrateAPI(apiURL)
	if err != nil {
		return types.Hash{}, err
	}

	meta, err := api.RPC.State.GetMetadataLatest()
	if err != nil {
		return types.Hash{}, err
	}

	//if app id is greater than 0 then it must be created before submitting data
	//err = errors.New("AppID can be 0")
	if appID == 0 {
		return types.Hash{}, err
	}

	c, err := types.NewCall(meta, "DataAvailability.submit_data", data)
	if err != nil {
		return types.Hash{}, err
	}

	//Create the extrinsic
	ext := types.NewExtrinsic(c)

	genesisHash, err := api.RPC.Chain.GetBlockHash(0)
	if err != nil {
		return types.Hash{}, err
	}

	rv, err := api.RPC.State.GetRuntimeVersionLatest()
	if err != nil {
		return types.Hash{}, err
	}

	keyringPair, err := signature.KeyringPairFromSecret(seed, 42)
	if err != nil {
		return types.Hash{}, err
	}

	key, err := types.CreateStorageKey(meta, "System", "Account", keyringPair.PublicKey)
	if err != nil {
		return types.Hash{}, err
	}

	var accountInfo types.AccountInfo
	ok, err := api.RPC.State.GetStorageLatest(key, &accountInfo)
	if err != nil || !ok {
		return types.Hash{}, err
	}

	nonce := uint32(accountInfo.Nonce)
	o := types.SignatureOptions{
		BlockHash:   genesisHash,
		Era:         types.ExtrinsicEra{IsMortalEra: false},
		GenesisHash: genesisHash,
		Nonce:       types.NewUCompactFromUInt(uint64(nonce)),
		SpecVersion: rv.SpecVersion,
		Tip:         types.NewUCompactFromUInt(0),
		AppID:       types.NewUCompactFromUInt(uint64(appID)),
		//AppID:       types.NewU32(uint32(appID)),
		//AppID:     types.U32(types.NewI16(int16(appID))),
		//AppID:     types.U32(uint32(appID)),
		//AppID:     types.U32(types.NewU16(uint16(appID))),
		TransactionVersion: rv.TransactionVersion,
	}

	// Sign the transaction using Alice's default account
	err = ext.Sign(keyringPair, o)
	if err != nil {
		return types.Hash{}, err
	}

	// Send the extrinsic
	hash, err := api.RPC.Author.SubmitExtrinsic(ext)
	if err != nil {
		return types.Hash{}, err
	}

	return hash, nil

}
