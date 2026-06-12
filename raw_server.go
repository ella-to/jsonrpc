package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// Server processes JSON-RPC requests over an io.ReadWriteCloser transport.
type RawServer struct {
	conn              io.ReadWriteCloser
	encoder           *json.Encoder
	decoder           *json.Decoder
	handler           Handler
	writeMu           sync.Mutex
	closed            chan struct{}
	closeErr          error
	closeMu           sync.Mutex
	contextPropagator ContextPropagator
}

type RawServerOpt interface {
	configureRawServer(*RawServer) error
}

// NewRawServer constructs a Server that uses handler to process incoming requests.
// Options can include WithContextPropagation to enable context propagation.
func NewRawServer(rwc io.ReadWriteCloser, handler Handler, opts ...RawServerOpt) *RawServer {
	if handler == nil {
		panic("jsonrpc: handler cannot be nil")
	}
	dec := json.NewDecoder(rwc)
	dec.UseNumber()

	server := &RawServer{
		conn:    rwc,
		encoder: json.NewEncoder(rwc),
		decoder: dec,
		handler: handler,
		closed:  make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		if err := opt.configureRawServer(server); err != nil {
			panic(err)
		}
	}

	return server
}

// Serve reads requests from the transport until the context is canceled or the
// connection terminates. It launches a goroutine per request so handler calls
// can run concurrently. The returned error is nil when the peer closes the
// connection cleanly.
func (s *RawServer) Serve(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			s.fail(ctx.Err())
			return ctx.Err()
		case <-s.closed:
			return s.CloseError()
		default:
		}

		var raw json.RawMessage
		if err := s.decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				s.fail(io.EOF)
				return nil
			}
			s.fail(err)
			return err
		}

		raw = json.RawMessage(bytes.TrimSpace(raw))
		if len(raw) == 0 {
			s.sendResponse(s.errorResponseWithNull(InvalidRequest, "invalid request", errors.New("empty payload")))
			continue
		}

		// Scope injected metadata to this message so it does not leak into
		// subsequent messages on the same connection.
		msgCtx := ctx

		// Try to extract metadata if context propagation is enabled
		if s.contextPropagator != nil && raw[0] == '{' {
			// Try to unwrap as a metadata wrapper
			var wrapper struct {
				Requests []json.RawMessage `json:"requests"`
				Metadata map[string]string `json:"metadata,omitempty"`
			}
			if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Requests) > 0 {
				// This is a wrapped batch request with metadata
				if len(wrapper.Metadata) > 0 {
					msgCtx = s.contextPropagator.Inject(msgCtx, wrapper.Metadata)
				}
				s.handleEntries(msgCtx, wrapper.Requests)
				continue
			}
		}

		if raw[0] == '[' {
			s.handleBatch(msgCtx, raw)
			continue
		}

		var req Request
		if err := json.Unmarshal(raw, &req); err != nil {
			s.sendResponse(s.errorResponseWithNull(InvalidRequest, "invalid request", err))
			continue
		}

		go func() {
			if resp := s.handleRequest(msgCtx, &req); resp != nil {
				s.sendResponse(resp)
			}
		}()
	}
}

// Close shuts down the server and closes the underlying transport.
func (s *RawServer) Close() error {
	s.closeMu.Lock()
	if s.closeErr != nil {
		err := s.closeErr
		s.closeMu.Unlock()
		return err
	}
	if err := s.conn.Close(); err != nil {
		s.closeErr = err
	} else {
		s.closeErr = io.EOF
	}
	s.closeOnce()
	err := s.closeErr
	s.closeMu.Unlock()
	return err
}

// CloseError reports the error that closed the server, if any.
func (s *RawServer) CloseError() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	return s.closeErr
}

func (s *RawServer) handleRequest(ctx context.Context, req *Request) *Response {
	if req.Method == "" {
		return s.errorResponse(req.ID, InvalidRequest, "method is required", nil)
	}
	if req.JSONRPC != Version {
		return s.errorResponse(req.ID, InvalidRequest, "invalid JSON-RPC version", nil)
	}

	resp := s.handler.Handle(ctx, req)

	if req.ID == nil {
		return nil
	}

	return resp
}

func (s *RawServer) handleBatch(ctx context.Context, raw json.RawMessage) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		s.sendResponse(s.errorResponseWithNull(InvalidRequest, "invalid request", err))
		return
	}
	s.handleEntries(ctx, entries)
}

func (s *RawServer) handleEntries(ctx context.Context, entries []json.RawMessage) {
	if len(entries) == 0 {
		s.sendResponse(s.errorResponseWithNull(InvalidRequest, "invalid request", errors.New("empty batch")))
		return
	}

	// Each goroutine writes only to its own slot, so no synchronization beyond
	// the WaitGroup is needed.
	responses := make([]*Response, len(entries))
	var wg sync.WaitGroup

	for i, element := range entries {
		var req Request
		if err := json.Unmarshal(element, &req); err != nil {
			responses[i] = s.errorResponseWithNull(InvalidRequest, "invalid request", err)
			continue
		}
		wg.Add(1)
		go func(idx int, r *Request) {
			defer wg.Done()
			responses[idx] = s.handleRequest(ctx, r)
		}(i, &req)
	}

	wg.Wait()

	ordered := make([]*Response, 0, len(entries))
	for _, resp := range responses {
		if resp != nil {
			ordered = append(ordered, resp)
		}
	}

	s.sendBatch(ordered)
}

func (s *RawServer) sendResponse(resp *Response) {
	if resp == nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.encoder.Encode(resp); err != nil {
		s.fail(err)
	}
}

func (s *RawServer) sendBatch(resps []*Response) {
	if len(resps) == 0 {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.encoder.Encode(resps); err != nil {
		s.fail(err)
	}
}

func (s *RawServer) errorResponse(id any, code int, message string, cause error) *Response {
	if id == nil {
		return nil
	}
	return &Response{
		JSONRPC: Version,
		Error: &Error{
			Code:    code,
			Message: message,
			Cause:   cause,
		},
		ID: id,
	}
}

func (s *RawServer) errorResponseWithNull(code int, message string, cause error) *Response {
	return &Response{
		JSONRPC: Version,
		Error: &Error{
			Code:    code,
			Message: message,
			Cause:   cause,
		},
		ID: nil,
	}
}

func (s *RawServer) fail(err error) {
	if err == nil {
		err = io.EOF
	}
	s.closeMu.Lock()
	if s.closeErr == nil {
		s.closeErr = err
		_ = s.conn.Close()
	}
	s.closeOnce()
	s.closeMu.Unlock()
}

func (s *RawServer) closeOnce() {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
}
