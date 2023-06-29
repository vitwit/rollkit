package mock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	mux2 "github.com/gorilla/mux"

	"github.com/rollkit/celestia-openrpc/types/blob"
	"github.com/rollkit/celestia-openrpc/types/header"
	"github.com/rollkit/celestia-openrpc/types/sdk"
	mockda "github.com/rollkit/rollkit/da/mock"
	"github.com/rollkit/rollkit/log"
	"github.com/rollkit/rollkit/store"
	"github.com/rollkit/rollkit/types"
)

type ErrorCode int

type respError struct {
	Code    ErrorCode       `json:"code"`
	Message string          `json:"message"`
	Meta    json.RawMessage `json:"meta,omitempty"`
}

func (e *respError) Error() string {
	if e.Code >= -32768 && e.Code <= -32000 {
		return fmt.Sprintf("RPC error (%d): %s", e.Code, e.Message)
	}
	return e.Message
}

type request struct {
	Jsonrpc string            `json:"jsonrpc"`
	ID      interface{}       `json:"id,omitempty"`
	Method  string            `json:"method"`
	Params  json.RawMessage   `json:"params"`
	Meta    map[string]string `json:"meta,omitempty"`
}

type response struct {
	Jsonrpc string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	ID      interface{} `json:"id"`
	Error   *respError  `json:"error,omitempty"`
}

// Server mocks celestia-node HTTP API.
type Server struct {
	mock      *mockda.DataAvailabilityLayerClient
	blockTime time.Duration
	server    *http.Server
	logger    log.Logger
}

// NewServer creates new instance of Server.
func NewServer(blockTime time.Duration, logger log.Logger) *Server {
	return &Server{
		mock:      new(mockda.DataAvailabilityLayerClient),
		blockTime: blockTime,
		logger:    logger,
	}
}

// Start starts HTTP server with given listener.
func (s *Server) Start(listener net.Listener) error {
	kvStore, err := store.NewDefaultInMemoryKVStore()
	if err != nil {
		return err
	}
	err = s.mock.Init([8]byte{}, []byte(s.blockTime.String()), kvStore, s.logger)
	if err != nil {
		return err
	}
	err = s.mock.Start()
	if err != nil {
		return err
	}
	go func() {
		s.server = new(http.Server)
		s.server.Handler = s.getHandler()
		err := s.server.Serve(listener)
		s.logger.Debug("http server exited with", "error", err)
	}()
	return nil
}

// Stop shuts down the Server.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

func (s *Server) getHandler() http.Handler {
	mux := mux2.NewRouter()
	mux.HandleFunc("/", s.rpc).Methods(http.MethodPost)

	return mux
}

func (s *Server) parseParams(w http.ResponseWriter, req request) []interface{} {
	var params []interface{}
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		s.writeError(w, err)
		return nil
	}
	return params
}

func (s *Server) returnResponse(w http.ResponseWriter, resp *response) {
	bytes, err := json.Marshal(resp)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeResponse(w, bytes)
}

func (s *Server) rpc(w http.ResponseWriter, r *http.Request) {
	var req request
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	switch req.Method {
	case "header.GetByHeight":
		params := s.parseParams(w, req)
		height := uint64(params[0].(float64))
		dah := s.mock.GetHeaderByHeight(height)
		resp := &response{
			Jsonrpc: "2.0",
			Result: header.ExtendedHeader{
				DAH: dah,
			},
			ID:    req.ID,
			Error: nil,
		}
		s.returnResponse(w, resp)
	case "blob.GetAll":
		params := s.parseParams(w, req)
		height := params[0].(float64)
		block := s.mock.RetrieveBlocks(r.Context(), uint64(height))
		blobs := make([]blob.Blob, len(block.Blocks))
		for i, block := range block.Blocks {
			data, err := block.MarshalBinary()
			if err != nil {
				s.writeError(w, err)
				return
			}
			blobs[i].Data = data

		}
		resp := &response{
			Jsonrpc: "2.0",
			Result:  blobs,
			ID:      req.ID,
			Error:   nil,
		}
		s.returnResponse(w, resp)
	case "share.SharesAvailable":
		resp := &response{
			Jsonrpc: "2.0",
			ID:      req.ID,
			Error:   nil,
		}
		s.returnResponse(w, resp)
	case "state.SubmitPayForBlob":
		params := s.parseParams(w, req)
		if len(params) != 3 {
			s.writeError(w, errors.New("expected 3 params"))
			return
		}
		block := types.Block{}
		blockBase64 := params[2].([]interface{})[0].(map[string]interface{})["data"].(string)
		blockData, err := base64.StdEncoding.DecodeString(blockBase64)
		if err != nil {
			s.writeError(w, err)
			return
		}
		err = block.UnmarshalBinary(blockData)
		if err != nil {
			s.writeError(w, err)
			return
		}

		res := s.mock.SubmitBlock(r.Context(), &block)
		resp := &response{
			Jsonrpc: "2.0",
			Result: &sdk.TxResponse{
				Height: int64(res.DAHeight),
			},
			ID:    req.ID,
			Error: nil,
		}
		s.returnResponse(w, resp)
	}
}

func (s *Server) writeResponse(w http.ResponseWriter, payload []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(payload)
	if err != nil {
		s.logger.Error("failed to write response", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	resp, jerr := json.Marshal(err.Error())
	if jerr != nil {
		s.logger.Error("failed to serialize error message", "error", jerr)
	}
	_, werr := w.Write(resp)
	if werr != nil {
		s.logger.Error("failed to write response", "error", werr)
	}
}
