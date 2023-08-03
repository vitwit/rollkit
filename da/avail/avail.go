package avail

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	ds "github.com/ipfs/go-datastore"
	openrpc "github.com/rollkit/celestia-openrpc"
	openrpcns "github.com/rollkit/celestia-openrpc/types/namespace"
	"github.com/rollkit/rollkit/da"
	"github.com/rollkit/rollkit/da/avail/datasubmit"
	"github.com/rollkit/rollkit/log"
	"github.com/rollkit/rollkit/types"
)

type Config struct {
	Seed   string `json:"seed"`
	ApiURL string `json:"api_url"`
	Size   int    `json:"size"`
	AppID  int    `json:"app_id"`
}

type DataAvailabilityLayerClient struct {
	rpc       *openrpc.Client
	namespace openrpcns.Namespace
	config    Config
	logger    log.Logger
}

var _ da.DataAvailabilityLayerClient = &DataAvailabilityLayerClient{}
var _ da.BlockRetriever = &DataAvailabilityLayerClient{}

// Init initializes DataAvailabilityLayerClient instance.
func (c *DataAvailabilityLayerClient) Init(namespaceID types.NamespaceID, config []byte, kvStore ds.Datastore, logger log.Logger) error {
	c.logger = logger

	if len(config) > 0 {
		return json.Unmarshal(config, &c.config)
	}

	return nil
}

// Start prepares DataAvailabilityLayerClient to work.
func (c *DataAvailabilityLayerClient) Start() error {
	c.logger.Info("starting avail Data Availability Layer Client", "baseURL", c.config.ApiURL)

	return nil
}

// Stop stops DataAvailabilityLayerClient.
func (c *DataAvailabilityLayerClient) Stop() error {
	c.logger.Info("stopping Avail Data Availability Layer Client")
	return nil
}

// SubmitBlock submits a block to DA layer.
func (c *DataAvailabilityLayerClient) SubmitBlock(ctx context.Context, block *types.Block) da.ResultSubmitBlock {

	data, err := block.MarshalBinary()
	if err != nil {
		return da.ResultSubmitBlock{
			BaseResult: da.BaseResult{
				Code:    da.StatusError,
				Message: err.Error(),
			},
		}
	}

	txHash, err := datasubmit.SubmitData(1000, c.config.ApiURL, c.config.Seed, c.config.AppID, data)

	if err != nil {
		return da.ResultSubmitBlock{
			BaseResult: da.BaseResult{
				Code:    da.StatusError,
				Message: err.Error(),
			},
		}
	}

	return da.ResultSubmitBlock{
		BaseResult: da.BaseResult{
			Code:     da.StatusSuccess,
			Message:  "tx hash: " + hex.EncodeToString(txHash[:]),
			DAHeight: 1,
		},
	}
}

// CheckBlockAvailability queries DA layer to check data availability of block.
func (a *DataAvailabilityLayerClient) CheckBlockAvailability(ctx context.Context, dataLayerHeight uint64) da.ResultCheckBlock {

	type Confidence struct {
		Block                uint32  `json:"block"`
		Confidence           float64 `json:"confidence"`
		SerialisedConfidence *string `json:"serialised_confidence,omitempty"`
	}

	blockNumber := dataLayerHeight
	confidenceURL := fmt.Sprintf("http://localhost:7000/v1/confidence/%d", blockNumber)

	response, err := http.Get(confidenceURL)

	if err != nil {
		return da.ResultCheckBlock{
			BaseResult: da.BaseResult{
				Code:    da.StatusError,
				Message: err.Error(),
			},
		}
	}

	responseData, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return da.ResultCheckBlock{
			BaseResult: da.BaseResult{
				Code:    da.StatusError,
				Message: err.Error(),
			},
		}
	}

	var confidenceObject Confidence
	json.Unmarshal(responseData, &confidenceObject)

	return da.ResultCheckBlock{
		BaseResult: da.BaseResult{
			Code:     da.StatusSuccess,
			DAHeight: uint64(confidenceObject.Block),
		},
		DataAvailable: confidenceObject.Confidence > 92,
	}
}

//RetrieveBlocks gets the block from DA layer.

func (c *DataAvailabilityLayerClient) RetrieveBlocks(ctx context.Context, dataLayerHeight uint64) da.ResultRetrieveBlocks {

	type AppData struct {
		Block      uint32 `json:"block"`
		Extrinsics string `json:"extrinsics"`
	}

	blocks := make([]*types.Block, 1)
	blocks[0] = new(types.Block)

	blockNumber := dataLayerHeight
	appDataURL := fmt.Sprintf("http://localhost:7000/v1/appdata/%d?decode=true", blockNumber)
	response, err := http.Get(appDataURL)
	if err != nil {
		return da.ResultRetrieveBlocks{
			BaseResult: da.BaseResult{
				Code:    da.StatusError,
				Message: err.Error(),
			},
		}
	}
	responseData, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return da.ResultRetrieveBlocks{
			BaseResult: da.BaseResult{
				Code:    da.StatusError,
				Message: err.Error(),
			},
		}
	}
	var appDataObject AppData
	json.Unmarshal(responseData, &appDataObject)

	return da.ResultRetrieveBlocks{
		BaseResult: da.BaseResult{
			Code:     da.StatusSuccess,
			DAHeight: uint64(appDataObject.Block),
			Message:  "block data: " + appDataObject.Extrinsics,
		},
		Blocks: blocks,
	}
}
