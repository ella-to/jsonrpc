package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// RawClient implements a JSON-RPC 2.0 client over an io.ReadWriteCloser transport.
type RawClient struct {
	conn              io.ReadWriteCloser
	encoder           *json.Encoder
	decoder           *json.Decoder
	pending           map[string]chan callResult
	pendMu            sync.Mutex
	writeMu           sync.Mutex
	closed            chan struct{}
	closeErr          error
	closeMu           sync.Mutex
	contextPropagator ContextPropagator
}

var _ Caller = (*RawClient)(nil)

type callResult struct {
	resp *Response
	err  error
}

type RawClientOpt interface {
	configureRawClient(*RawClient) error
}

// NewRawClient constructs a Client that communicates over rwc.
// Options can include WithContextPropagation to enable context propagation.
func NewRawClient(rwc io.ReadWriteCloser, opts ...RawClientOpt) *RawClient {
	dec := json.NewDecoder(rwc)
	dec.UseNumber()
	c := &RawClient{
		conn:    rwc,
		encoder: json.NewEncoder(rwc),
		decoder: dec,
		pending: make(map[string]chan callResult),
		closed:  make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		err := opt.configureRawClient(c)
		if err != nil {
			panic(err)
		}
	}

	go c.readLoop()
	return c
}

// Call sends the provided requests and returns their corresponding responses. The
// payload is encoded as a JSON array even when only a single request is supplied.
// Responses are aligned with the order of the supplied requests, and notifications
// (requests without an id) yield nil entries in the returned slice.
func (c *RawClient) Call(ctx context.Context, requests ...*Request) ([]*Response, error) {
	if len(requests) == 0 {
		return nil, nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, c.CloseError()
	default:
	}

	type pendingEntry struct {
		id  string
		idx int
		ch  chan callResult
	}

	responses := make([]*Response, len(requests))
	pendings := make([]pendingEntry, 0, len(requests))

	cleanup := func() {
		for _, p := range pendings {
			c.removePending(p.id)
		}
	}

	for i, req := range requests {
		if req == nil {
			cleanup()
			return nil, fmt.Errorf("jsonrpc2: request at index %d is nil", i)
		}
		if req.JSONRPC == "" {
			req.JSONRPC = Version
		}
		if req.ID == nil {
			continue
		}
		idKey, ok := NormalizeID(req.ID)
		if !ok {
			cleanup()
			return nil, fmt.Errorf("jsonrpc2: invalid request id at index %d", i)
		}
		respCh := make(chan callResult, 1)
		c.pendMu.Lock()
		if _, exists := c.pending[idKey]; exists {
			c.pendMu.Unlock()
			cleanup()
			return nil, fmt.Errorf("jsonrpc2: duplicate request id %s", idKey)
		}
		c.pending[idKey] = respCh
		c.pendMu.Unlock()
		pendings = append(pendings, pendingEntry{id: idKey, idx: i, ch: respCh})
	}

	if err := c.sendRequests(ctx, requests); err != nil {
		cleanup()
		return nil, err
	}

	for _, pending := range pendings {
		select {
		case <-ctx.Done():
			cleanup()
			return nil, ctx.Err()
		case <-c.closed:
			cleanup()
			return nil, c.CloseError()
		case res := <-pending.ch:
			if res.err != nil {
				cleanup()
				return nil, res.err
			}
			responses[pending.idx] = res.resp
		}
	}

	return responses, nil
}

// Close terminates the underlying transport and releases resources.
func (c *RawClient) Close() error {
	c.closeMu.Lock()
	if c.closeErr != nil {
		err := c.closeErr
		c.closeMu.Unlock()
		return err
	}
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	if err := c.conn.Close(); err != nil {
		c.closeErr = err
	} else {
		c.closeErr = io.EOF
	}
	cerr := c.closeErr
	c.closeMu.Unlock()
	c.failPending(cerr)
	return cerr
}

// CloseError reports the error that caused the client to close, if any.
func (c *RawClient) CloseError() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closeErr
}

func (c *RawClient) readLoop() {
	for {
		var raw json.RawMessage
		if err := c.decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				c.failPending(io.EOF)
			} else {
				c.failPending(err)
			}
			return
		}

		raw = json.RawMessage(bytes.TrimSpace(raw))
		if len(raw) == 0 {
			continue
		}

		if raw[0] == '[' {
			var batch []Response
			if err := json.Unmarshal(raw, &batch); err != nil {
				c.failPending(err)
				return
			}
			for i := range batch {
				c.dispatchResponse(&batch[i])
			}
			continue
		}

		var resp Response
		if err := json.Unmarshal(raw, &resp); err != nil {
			c.failPending(err)
			return
		}
		c.dispatchResponse(&resp)
	}
}

func (c *RawClient) sendRequests(ctx context.Context, requests []*Request) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// If context propagation is enabled, wrap requests with metadata
	if c.contextPropagator != nil {
		metadata := c.contextPropagator.Extract(ctx)
		if len(metadata) > 0 {
			// Send a wrapper with metadata
			wrapper := struct {
				Requests []*Request        `json:"requests"`
				Metadata map[string]string `json:"metadata,omitempty"`
			}{
				Requests: requests,
				Metadata: metadata,
			}
			return c.encoder.Encode(wrapper)
		}
	}

	// No context propagation or no metadata, send requests as-is
	return c.encoder.Encode(requests)
}

func (c *RawClient) dispatchResponse(resp *Response) {
	if resp.JSONRPC != Version {
		c.failPending(&Error{Code: InvalidRequest, Message: "invalid JSON-RPC version"})
		return
	}
	if resp.ID == nil {
		return
	}
	idKey, ok := NormalizeID(resp.ID)
	if !ok {
		return
	}
	c.pendMu.Lock()
	ch, exists := c.pending[idKey]
	if exists {
		delete(c.pending, idKey)
	}
	c.pendMu.Unlock()
	if exists {
		respCopy := *resp
		ch <- callResult{resp: &respCopy}
	}
}

func (c *RawClient) removePending(id string) chan callResult {
	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	return ch
}

func (c *RawClient) failPending(err error) {
	if err == nil {
		err = io.EOF
	}

	c.closeMu.Lock()
	if c.closeErr == nil {
		c.closeErr = err
	} else {
		err = c.closeErr
	}
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	c.closeMu.Unlock()

	c.pendMu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- callResult{err: err}
	}
	c.pendMu.Unlock()
}
