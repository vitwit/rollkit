package mock

import (
	"fmt"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

func SubmitData(size int, apiURL string, seed string, AppID int, data []byte) error {
	api, err := gsrpc.NewSubstrateAPI(apiURL)
	if err != nil {
		return fmt.Errorf("cannot create api:%w", err)
	}

	meta, err := api.RPC.State.GetMetadataLatest()
	if err != nil {
		return fmt.Errorf("cannot get metadata:%w", err)
	}

	// Set data and appID according to need
	// data, _ := RandToken(size)

	//var appID types.AppID

	var appID int

	// if app id is greater than 0 then it must be created before submitting data
	if AppID != 0 {
		appID = AppID
	}

	c, err := types.NewCall(meta, "DataAvailability.submit_data", data)
	if err != nil {
		return fmt.Errorf("cannot create new call:%w", err)
	}
	// fmt.Println(c)

	//Create the extrinsic
	ext := types.NewExtrinsic(c)

	genesisHash, err := api.RPC.Chain.GetBlockHash(0)
	if err != nil {
		return fmt.Errorf("cannot get block hash:%w", err)
	}

	rv, err := api.RPC.State.GetRuntimeVersionLatest()
	if err != nil {
		return fmt.Errorf("cannot get runtime version:%w", err)
	}

	keyringPair, err := signature.KeyringPairFromSecret(seed, 42)
	if err != nil {
		return fmt.Errorf("cannot create keyPair:%w", err)
	}

	key, err := types.CreateStorageKey(meta, "System", "Account", keyringPair.PublicKey)
	if err != nil {
		return fmt.Errorf("cannot create storage key:%w", err)
	}

	var accountInfo types.AccountInfo
	ok, err := api.RPC.State.GetStorageLatest(key, &accountInfo)
	if err != nil || !ok {
		return fmt.Errorf("cannot get latest storage:%w", err)
	}

	// fmt.Println(accountInfo)

	nonce := uint32(accountInfo.Nonce)
	o := types.SignatureOptions{
		BlockHash:          genesisHash,
		Era:                types.ExtrinsicEra{IsMortalEra: false},
		GenesisHash:        genesisHash,
		Nonce:              types.NewUCompactFromUInt(uint64(nonce)),
		SpecVersion:        rv.SpecVersion,
		Tip:                types.NewUCompactFromUInt(0),
		AppID:              types.NewUCompactFromUInt(uint64(appID)),
		TransactionVersion: rv.TransactionVersion,
	}

	// Sign the transaction using Alice's default account
	err = ext.Sign(keyringPair, o)
	if err != nil {
		return fmt.Errorf("cannot sign:%w", err)
	}

	// Send the extrinsic
	hash, err := api.RPC.Author.SubmitExtrinsic(ext)
	if err != nil {
		return fmt.Errorf("cannot submit extrinsic:%w", err)
	}
	fmt.Printf("Data submitted: %v against appID %v  sent with hash %#x\n", data, appID, hash)

	return nil

}

// RandToken generates a random hex value.
// func RandToken(n int) (string, error) {
// 	bytes := make([]byte, n)
// 	if _, err := rand.Read(bytes); err != nil {
// 		return "", err
// 	}
// 	return hex.EncodeToString(bytes), nil
// }

// getData extracts data from the block and compares it
// func GetData(hash types.Hash, api *gsrpc.SubstrateAPI, data string) error {
// 	block, err := api.RPC.Chain.GetBlock(hash)
// 	if err != nil {
// 		return fmt.Errorf("cannot get block by hash:%w", err)
// 	}
// 	for _, ext := range block.Block.Extrinsics {
// 		// these values below are specific indexes only for data submission, differs with each extrinsic
// 		if ext.Method.CallIndex.SectionIndex == 29 && ext.Method.CallIndex.MethodIndex == 1 {
// 			arg := ext.Method.Args
// 			str := string(arg)
// 			slice := str[2:]
// 			fmt.Println("string value", slice)
// 			fmt.Println("data", data)
// 			if slice == data {
// 				fmt.Println("Data found in block")
// 			}
// 		}
// 	}
// 	return nil
// }
