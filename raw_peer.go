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

// RawPeer implements a bidirectional JSON-RPC 2.0 peer that can both send
// requests (as a client) and handle incoming requests (as a server) over a
// single io.ReadWriteCloser connection. This enables nested/hierarchical call
// patterns where a handler can make outgoing calls without deadlocking.
type RawPeer struct {
	conn     io.ReadWriteCloser
	encoder  *json.Encoder
	decoder  *json.Decoder
	handler  Handler
	pending  map[string]chan callResult
	pendMu   sync.Mutex
	writeMu  sync.Mutex
	closed   chan struct{}
	closeErr error
	closeMu  sync.Mutex
}

var _ Caller = (*RawPeer)(nil)

// NewRawPeer creates a new bidirectional peer that uses the given handler
// to process incoming requests. The peer can also make outgoing calls via
// the Call method. If handler is nil, incoming requests will receive a
// MethodNotFound error until SetHandler is called.
func NewRawPeer(rwc io.ReadWriteCloser, handler Handler) *RawPeer {
	if handler == nil {
		handler = HandlerFunc(func(ctx context.Context, req *Request) *Response {
			return req.CreateErrorResponse(NewError(MethodNotFound, "no handler"))
		})
	}
	dec := json.NewDecoder(rwc)
	dec.UseNumber()
	return &RawPeer{
		conn:    rwc,
		encoder: json.NewEncoder(rwc),
		decoder: dec,
		handler: handler,
		pending: make(map[string]chan callResult),
		closed:  make(chan struct{}),
	}
}

// SetHandler sets the handler for incoming requests. This can be called
// before Serve to configure the handler after peer creation.
func (p *RawPeer) SetHandler(handler Handler) {
	p.handler = handler
}

// Serve starts the read loop that processes incoming messages. It dispatches
// incoming requests to the handler and routes incoming responses to pending
// outgoing calls. Serve blocks until the context is canceled or the connection
// is closed. It is safe to call Call from within a handler.
func (p *RawPeer) Serve(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			p.fail(ctx.Err())
			return ctx.Err()
		case <-p.closed:
			return p.CloseError()
		default:
		}

		var raw json.RawMessage
		if err := p.decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				p.fail(io.EOF)
				return nil
			}
			p.fail(err)
			return err
		}

		raw = json.RawMessage(bytes.TrimSpace(raw))
		if len(raw) == 0 {
			continue
		}

		// Determine if this is a request, response, or batch
		if raw[0] == '[' {
			p.handleBatch(ctx, raw)
			continue
		}

		// Try to determine if it's a request or response by peeking at the structure
		msgType := p.classifyMessage(raw)
		switch msgType {
		case messageTypeRequest:
			var req Request
			if err := json.Unmarshal(raw, &req); err != nil {
				p.sendResponse(&Response{
					JSONRPC: Version,
					Error:   &Error{Code: InvalidRequest, Message: "invalid request"},
				})
				continue
			}
			go p.handleRequest(ctx, &req)

		case messageTypeResponse:
			var resp Response
			if err := json.Unmarshal(raw, &resp); err != nil {
				continue
			}
			p.dispatchResponse(&resp)

		default:
			p.sendResponse(&Response{
				JSONRPC: Version,
				Error:   &Error{Code: InvalidRequest, Message: "invalid message"},
			})
		}
	}
}

type messageType int

const (
	messageTypeUnknown messageType = iota
	messageTypeRequest
	messageTypeResponse
)

// classifyMessage determines if a raw JSON message is a request or response.
// Requests have a "method" field, responses have "result" or "error" fields.
func (p *RawPeer) classifyMessage(raw json.RawMessage) messageType {
	// A non-nil json.RawMessage means the field was present, even if null.
	var probe struct {
		Method json.RawMessage `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
		ID     json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return messageTypeUnknown
	}

	// If it has a method field, it's a request.
	if probe.Method != nil {
		return messageTypeRequest
	}

	// Responses have either result, error, or id fields and no method.
	if probe.Result != nil || probe.Error != nil || probe.ID != nil {
		return messageTypeResponse
	}

	return messageTypeUnknown
}

// Call sends one or more JSON-RPC requests and waits for their responses.
// Notifications (requests without an ID) will have nil entries in the returned
// slice. It is safe to call this from within a request handler.
func (p *RawPeer) Call(ctx context.Context, requests ...*Request) ([]*Response, error) {
	if len(requests) == 0 {
		return nil, nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closed:
		return nil, p.CloseError()
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
		for _, pe := range pendings {
			p.removePending(pe.id)
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
		p.pendMu.Lock()
		if _, exists := p.pending[idKey]; exists {
			p.pendMu.Unlock()
			cleanup()
			return nil, fmt.Errorf("jsonrpc2: duplicate request id %s", idKey)
		}
		p.pending[idKey] = respCh
		p.pendMu.Unlock()
		pendings = append(pendings, pendingEntry{id: idKey, idx: i, ch: respCh})
	}

	if err := p.sendRequests(requests); err != nil {
		cleanup()
		return nil, err
	}

	for _, pending := range pendings {
		select {
		case <-ctx.Done():
			cleanup()
			return nil, ctx.Err()
		case <-p.closed:
			cleanup()
			return nil, p.CloseError()
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

// Close terminates the peer and closes the underlying transport.
func (p *RawPeer) Close() error {
	p.closeMu.Lock()
	if p.closeErr != nil {
		err := p.closeErr
		p.closeMu.Unlock()
		return err
	}
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	if err := p.conn.Close(); err != nil {
		p.closeErr = err
	} else {
		p.closeErr = io.EOF
	}
	cerr := p.closeErr
	p.closeMu.Unlock()
	p.failPending(cerr)
	return cerr
}

// CloseError reports the error that caused the peer to close, if any.
func (p *RawPeer) CloseError() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	return p.closeErr
}

func (p *RawPeer) handleRequest(ctx context.Context, req *Request) {
	if req.Method == "" {
		p.sendResponse(&Response{
			JSONRPC: Version,
			Error:   &Error{Code: InvalidRequest, Message: "method is required"},
			ID:      req.ID,
		})
		return
	}
	if req.JSONRPC != Version {
		p.sendResponse(&Response{
			JSONRPC: Version,
			Error:   &Error{Code: InvalidRequest, Message: "invalid JSON-RPC version"},
			ID:      req.ID,
		})
		return
	}

	resp := p.handler.Handle(ctx, req)

	// Notifications don't get responses
	if req.ID == nil {
		return
	}

	if resp != nil {
		p.sendResponse(resp)
	}
}

func (p *RawPeer) handleBatch(ctx context.Context, raw json.RawMessage) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		p.sendResponse(&Response{
			JSONRPC: Version,
			Error:   &Error{Code: InvalidRequest, Message: "invalid request", Cause: err},
		})
		return
	}
	if len(entries) == 0 {
		p.sendResponse(&Response{
			JSONRPC: Version,
			Error:   &Error{Code: InvalidRequest, Message: "empty batch"},
		})
		return
	}

	// Separate requests from responses in the batch
	var requestEntries []struct {
		idx int
		raw json.RawMessage
	}
	var responseEntries []json.RawMessage

	for i, entry := range entries {
		msgType := p.classifyMessage(entry)
		switch msgType {
		case messageTypeRequest:
			requestEntries = append(requestEntries, struct {
				idx int
				raw json.RawMessage
			}{idx: i, raw: entry})
		case messageTypeResponse:
			responseEntries = append(responseEntries, entry)
		default:
			// Invalid entry - we'll handle as invalid request below
			requestEntries = append(requestEntries, struct {
				idx int
				raw json.RawMessage
			}{idx: i, raw: entry})
		}
	}

	// Dispatch responses immediately
	for _, respRaw := range responseEntries {
		var resp Response
		if err := json.Unmarshal(respRaw, &resp); err != nil {
			continue
		}
		p.dispatchResponse(&resp)
	}

	// Handle requests asynchronously - don't block the serve loop!
	// This allows handlers to make nested calls.
	if len(requestEntries) > 0 {
		go p.handleBatchRequests(ctx, entries, requestEntries)
	}
}

func (p *RawPeer) handleBatchRequests(ctx context.Context, entries []json.RawMessage, requestEntries []struct {
	idx int
	raw json.RawMessage
},
) {
	// Each goroutine writes only to its own slot, so no synchronization beyond
	// the WaitGroup is needed.
	responses := make([]*Response, len(entries))
	var wg sync.WaitGroup

	for _, entry := range requestEntries {
		var req Request
		if err := json.Unmarshal(entry.raw, &req); err != nil {
			responses[entry.idx] = &Response{
				JSONRPC: Version,
				Error:   &Error{Code: InvalidRequest, Message: "invalid request", Cause: err},
			}
			continue
		}
		wg.Add(1)
		go func(idx int, r *Request) {
			defer wg.Done()
			if r.Method == "" || r.JSONRPC != Version {
				if r.ID != nil {
					responses[idx] = &Response{
						JSONRPC: Version,
						Error:   &Error{Code: InvalidRequest, Message: "invalid request"},
						ID:      r.ID,
					}
				}
				return
			}
			resp := p.handler.Handle(ctx, r)
			if r.ID != nil && resp != nil {
				responses[idx] = resp
			}
		}(entry.idx, &req)
	}

	wg.Wait()

	ordered := make([]*Response, 0, len(requestEntries))
	for _, resp := range responses {
		if resp != nil {
			ordered = append(ordered, resp)
		}
	}

	if len(ordered) > 0 {
		p.sendBatch(ordered)
	}
}

func (p *RawPeer) sendResponse(resp *Response) {
	if resp == nil {
		return
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if err := p.encoder.Encode(resp); err != nil {
		p.fail(err)
	}
}

func (p *RawPeer) sendBatch(resps []*Response) {
	if len(resps) == 0 {
		return
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if err := p.encoder.Encode(resps); err != nil {
		p.fail(err)
	}
}

func (p *RawPeer) sendRequests(requests []*Request) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.encoder.Encode(requests)
}

func (p *RawPeer) dispatchResponse(resp *Response) {
	if resp.JSONRPC != Version {
		return
	}
	if resp.ID == nil {
		return
	}
	idKey, ok := NormalizeID(resp.ID)
	if !ok {
		return
	}
	p.pendMu.Lock()
	ch, exists := p.pending[idKey]
	if exists {
		delete(p.pending, idKey)
	}
	p.pendMu.Unlock()
	if exists {
		respCopy := *resp
		ch <- callResult{resp: &respCopy}
	}
}

func (p *RawPeer) removePending(id string) chan callResult {
	p.pendMu.Lock()
	defer p.pendMu.Unlock()
	ch, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	return ch
}

func (p *RawPeer) fail(err error) {
	if err == nil {
		err = io.EOF
	}
	p.closeMu.Lock()
	if p.closeErr == nil {
		p.closeErr = err
		_ = p.conn.Close()
	}
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	p.closeMu.Unlock()
}

func (p *RawPeer) failPending(err error) {
	if err == nil {
		err = io.EOF
	}

	p.closeMu.Lock()
	if p.closeErr == nil {
		p.closeErr = err
	} else {
		err = p.closeErr
	}
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	p.closeMu.Unlock()

	p.pendMu.Lock()
	for id, ch := range p.pending {
		delete(p.pending, id)
		ch <- callResult{err: err}
	}
	p.pendMu.Unlock()
}

// NewRawPeerPair creates two connected peers for testing or in-process
// communication. Each peer uses the provided handler to process incoming
// requests from the other peer.
func NewRawPeerPair(handler1, handler2 Handler) (peer1, peer2 *RawPeer) {
	conn1, conn2 := newAsyncPipe()
	peer1 = NewRawPeer(conn1, handler1)
	peer2 = NewRawPeer(conn2, handler2)
	return peer1, peer2
}

// asyncPipe provides a non-blocking bidirectional pipe using buffered channels.
// This prevents deadlocks when both peers need to send messages simultaneously.
type asyncPipe struct {
	readCh    chan []byte
	writeCh   chan []byte
	closeCh   chan struct{}
	closeOnce sync.Once
	buf       []byte
	mu        sync.Mutex
}

func (p *asyncPipe) Read(b []byte) (int, error) {
	// First drain any buffered data
	p.mu.Lock()
	if len(p.buf) > 0 {
		n := copy(b, p.buf)
		p.buf = p.buf[n:]
		p.mu.Unlock()
		return n, nil
	}
	p.mu.Unlock()

	select {
	case data, ok := <-p.readCh:
		if !ok {
			return 0, io.EOF
		}
		n := copy(b, data)
		if n < len(data) {
			p.mu.Lock()
			p.buf = append(p.buf, data[n:]...)
			p.mu.Unlock()
		}
		return n, nil
	case <-p.closeCh:
		return 0, io.EOF
	}
}

func (p *asyncPipe) Write(b []byte) (int, error) {
	select {
	case <-p.closeCh:
		return 0, io.ErrClosedPipe
	default:
	}

	// Make a copy to avoid data races
	data := make([]byte, len(b))
	copy(data, b)

	select {
	case p.writeCh <- data:
		return len(b), nil
	case <-p.closeCh:
		return 0, io.ErrClosedPipe
	}
}

func (p *asyncPipe) Close() error {
	p.closeOnce.Do(func() {
		close(p.closeCh)
	})
	return nil
}

func newAsyncPipe() (io.ReadWriteCloser, io.ReadWriteCloser) {
	// Use buffered channels to prevent blocking
	ch1 := make(chan []byte, 100)
	ch2 := make(chan []byte, 100)

	pipe1 := &asyncPipe{
		readCh:  ch1,
		writeCh: ch2,
		closeCh: make(chan struct{}),
	}
	pipe2 := &asyncPipe{
		readCh:  ch2,
		writeCh: ch1,
		closeCh: make(chan struct{}),
	}
	return pipe1, pipe2
}
