package mock

import (
	"bytes"
	"context"
	"encoding/hex"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	ds "github.com/ipfs/go-datastore"

	"github.com/rollkit/celestia-openrpc/types/core"
	"github.com/rollkit/rollkit/da"
	"github.com/rollkit/rollkit/log"
	"github.com/rollkit/rollkit/store"
	"github.com/rollkit/rollkit/types"
)

// DataAvailabilityLayerClient is intended only for usage in tests.
// It does actually ensures DA - it stores data in-memory.
type DataAvailabilityLayerClient struct {
	logger log.Logger
	dalcKV ds.Datastore

	daHeaders     map[uint64]*core.DataAvailabilityHeader
	daHeadersLock sync.RWMutex

	daHeight uint64
	config   config
}

const defaultBlockTime = 3 * time.Second

type config struct {
	BlockTime time.Duration
}

var _ da.DataAvailabilityLayerClient = &DataAvailabilityLayerClient{}
var _ da.BlockRetriever = &DataAvailabilityLayerClient{}

func getRandomHeader(size int) *core.DataAvailabilityHeader {
	randRowsRoots := make([][]byte, size)
	for i := 0; i < size; i++ {
		for j := 0; j < size; j++ {
			randRowsRoots[i] = make([]byte, size)
			rand.Read(randRowsRoots[i][:])
		}
	}
	randColumnRoots := make([][]byte, size)
	for i := 0; i < size; i++ {
		for j := 0; j < size; j++ {
			randColumnRoots[i] = make([]byte, size)
			rand.Read(randColumnRoots[i][:])
		}
	}
	return &core.DataAvailabilityHeader{
		RowsRoots:   randRowsRoots,
		ColumnRoots: randColumnRoots,
	}

}

// Init is called once to allow DA client to read configuration and initialize resources.
func (m *DataAvailabilityLayerClient) Init(_ types.NamespaceID, config []byte, dalcKV ds.Datastore, logger log.Logger) error {
	m.logger = logger
	m.dalcKV = dalcKV
	m.daHeight = 1
	m.daHeaders = make(map[uint64]*core.DataAvailabilityHeader)

	m.daHeadersLock.Lock()
	m.daHeaders[m.daHeight] = getRandomHeader(8)
	m.daHeadersLock.Unlock()

	if len(config) > 0 {
		var err error
		m.config.BlockTime, err = time.ParseDuration(string(config))
		if err != nil {
			return err
		}
	} else {
		m.config.BlockTime = defaultBlockTime
	}
	return nil
}

// Start implements DataAvailabilityLayerClient interface.
func (m *DataAvailabilityLayerClient) Start() error {
	m.logger.Debug("Mock Data Availability Layer Client starting")
	go func() {
		for {
			time.Sleep(m.config.BlockTime)
			m.updateDAHeight()
		}
	}()
	return nil
}

// Stop implements DataAvailabilityLayerClient interface.
func (m *DataAvailabilityLayerClient) Stop() error {
	m.logger.Debug("Mock Data Availability Layer Client stopped")
	return nil
}

// GetHeaderByHeight returns the header at the given height.
func (m *DataAvailabilityLayerClient) GetHeaderByHeight(height uint64) *core.DataAvailabilityHeader {
	m.daHeadersLock.RLock()
	dah := m.daHeaders[height]
	m.daHeadersLock.RUnlock()
	return dah
}

func isEqual(headerA, headerB *core.DataAvailabilityHeader) bool {
	if len(headerA.RowsRoots) != len(headerB.RowsRoots) {
		return false
	}
	if len(headerA.ColumnRoots) != len(headerB.ColumnRoots) {
		return false
	}
	for i, row := range headerA.RowsRoots {
		if !bytes.Equal(row, headerB.RowsRoots[i]) {
			return false
		}
	}
	for i, col := range headerA.ColumnRoots {
		if !bytes.Equal(col, headerB.ColumnRoots[i]) {
			return false
		}
	}
	return true
}

// GetHeightByHeader returns the height for the given header.
func (m *DataAvailabilityLayerClient) GetHeightByHeader(dah *core.DataAvailabilityHeader) uint64 {
	for height, header := range m.daHeaders {
		if isEqual(header, dah) {
			return height
		}
	}
	return 0
}

// SubmitBlock submits the passed in block to the DA layer.
// This should create a transaction which (potentially)
// triggers a state transition in the DA layer.
func (m *DataAvailabilityLayerClient) SubmitBlock(ctx context.Context, block *types.Block) da.ResultSubmitBlock {
	daHeight := atomic.LoadUint64(&m.daHeight)
	m.logger.Debug("Submitting block to DA layer!", "height", block.SignedHeader.Header.Height(), "dataLayerHeight", daHeight)

	hash := block.SignedHeader.Header.Hash()
	blob, err := block.MarshalBinary()
	if err != nil {
		return da.ResultSubmitBlock{BaseResult: da.BaseResult{Code: da.StatusError, Message: err.Error()}}
	}

	err = m.dalcKV.Put(ctx, getKey(daHeight, uint64(block.SignedHeader.Header.Height())), hash[:])
	if err != nil {
		return da.ResultSubmitBlock{BaseResult: da.BaseResult{Code: da.StatusError, Message: err.Error()}}
	}

	err = m.dalcKV.Put(ctx, ds.NewKey(hex.EncodeToString(hash[:])), blob)
	if err != nil {
		return da.ResultSubmitBlock{BaseResult: da.BaseResult{Code: da.StatusError, Message: err.Error()}}
	}

	return da.ResultSubmitBlock{
		BaseResult: da.BaseResult{
			Code:     da.StatusSuccess,
			Message:  "OK",
			DAHeight: daHeight,
		},
	}
}

// CheckBlockAvailability queries DA layer to check data availability of block corresponding to given header.
func (m *DataAvailabilityLayerClient) CheckBlockAvailability(ctx context.Context, daHeight uint64) da.ResultCheckBlock {
	blocksRes := m.RetrieveBlocks(ctx, daHeight)
	return da.ResultCheckBlock{BaseResult: da.BaseResult{Code: blocksRes.Code}, DataAvailable: len(blocksRes.Blocks) > 0}
}

// RetrieveBlocks returns block at given height from data availability layer.
func (m *DataAvailabilityLayerClient) RetrieveBlocks(ctx context.Context, daHeight uint64) da.ResultRetrieveBlocks {
	if daHeight >= atomic.LoadUint64(&m.daHeight) {
		return da.ResultRetrieveBlocks{BaseResult: da.BaseResult{Code: da.StatusError, Message: "block not found"}}
	}

	results, err := store.PrefixEntries(ctx, m.dalcKV, getPrefix(daHeight))
	if err != nil {
		return da.ResultRetrieveBlocks{BaseResult: da.BaseResult{Code: da.StatusError, Message: err.Error()}}
	}

	var blocks []*types.Block
	for result := range results.Next() {
		blob, err := m.dalcKV.Get(ctx, ds.NewKey(hex.EncodeToString(result.Entry.Value)))
		if err != nil {
			return da.ResultRetrieveBlocks{BaseResult: da.BaseResult{Code: da.StatusError, Message: err.Error()}}
		}

		block := &types.Block{}
		err = block.UnmarshalBinary(blob)
		if err != nil {
			return da.ResultRetrieveBlocks{BaseResult: da.BaseResult{Code: da.StatusError, Message: err.Error()}}
		}
		blocks = append(blocks, block)
	}

	return da.ResultRetrieveBlocks{BaseResult: da.BaseResult{Code: da.StatusSuccess}, Blocks: blocks}
}

func getPrefix(daHeight uint64) string {
	return store.GenerateKey([]interface{}{daHeight})
}

func getKey(daHeight uint64, height uint64) ds.Key {
	return ds.NewKey(store.GenerateKey([]interface{}{daHeight, height}))
}

func (m *DataAvailabilityLayerClient) updateDAHeight() {
	blockStep := rand.Uint64()%10 + 1 //nolint:gosec
	atomic.AddUint64(&m.daHeight, blockStep)
	m.daHeadersLock.Lock()
	m.daHeaders[m.daHeight] = getRandomHeader(8)
	m.daHeadersLock.Unlock()

}
