package jsonrpc

import (
	"bytes"
	"context"
	"ella.to/slogx"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type (
	httpCtxKey string
)

const (
	httpCtxKeyResp httpCtxKey = "jsonrpc2_http_context_key_resp"
)

func injectHttpResp(ctx context.Context, w http.ResponseWriter) context.Context {
	ctx = slogx.Context(ctx)
	return context.WithValue(ctx, httpCtxKeyResp, w)
}

func HttpRespFromContext(ctx context.Context) http.ResponseWriter {
	ctx = slogx.Context(ctx)
	val := ctx.Value(httpCtxKeyResp)
	resp, ok := val.(http.ResponseWriter)
	if !ok {
		return nil
	}
	return resp
}

type HttpHandlerOpt interface {
	configureHttpHandler(*httpServer) error
}

// NewHTTPHandler adapts a JSON-RPC Handler to a net/http.Handler.
// Options can include WithContextPropagation to enable context propagation.
func NewHTTPHandler(handler Handler, opts ...HttpHandlerOpt) http.Handler {
	if handler == nil {
		panic("jsonrpc2: handler cannot be nil")
	}

	server := &httpServer{handler: handler}

	// Apply options
	for _, opt := range opts {
		if err := opt.configureHttpHandler(server); err != nil {
			panic(err)
		}
	}

	return server
}

// HTTPHandler is deprecated. Use NewHTTPHandler instead.
func HTTPHandler(handler Handler) http.Handler {
	return NewHTTPHandler(handler)
}

type httpServer struct {
	handler           Handler
	contextPropagator ContextPropagator
}

func (s *httpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, s.errorResponseWithNull(ParseError, "failed to read request body", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		s.writeJSON(w, s.errorResponseWithNull(InvalidRequest, "invalid request", errors.New("empty payload")))
		return
	}

	ctx := injectHttpResp(r.Context(), w)

	// Extract and inject context metadata from HTTP headers
	if s.contextPropagator != nil {
		metadata := make(map[string]string)
		const metaPrefix = "X-Rpc-Meta-"
		for key, values := range r.Header {
			if len(values) > 0 {
				// HTTP headers are case-insensitive, Go canonicalizes them
				canonicalKey := http.CanonicalHeaderKey(key)
				if len(canonicalKey) > len(metaPrefix) {
					// Check if this header starts with our metadata prefix
					if canonicalKey[:len(metaPrefix)] == metaPrefix {
						// Extract the key part and convert to lowercase to match original key names
						// e.g., "Request-Id" becomes "request-id"
						actualKey := canonicalKey[len(metaPrefix):]
						normalizedKey := strings.ToLower(actualKey)
						metadata[normalizedKey] = values[0]
					}
				}
			}
		}
		if len(metadata) > 0 {
			ctx = s.contextPropagator.Inject(ctx, metadata)
		}
	}

	if trimmed[0] == '[' {
		s.handleBatch(ctx, w, trimmed)
		return
	}

	var req Request
	if err := json.Unmarshal(trimmed, &req); err != nil {
		s.writeJSON(w, s.errorResponseWithNull(InvalidRequest, "invalid request", err))
		return
	}

	if resp := s.handleRequest(ctx, &req); resp != nil {
		s.writeJSON(w, resp)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *httpServer) handleBatch(ctx context.Context, w http.ResponseWriter, raw json.RawMessage) {
	ctx = slogx.Context(ctx)
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		s.writeJSON(w, s.errorResponseWithNull(InvalidRequest, "invalid request", err))
		return
	}
	if len(entries) == 0 {
		s.writeJSON(w, s.errorResponseWithNull(InvalidRequest, "invalid request", errors.New("empty batch")))
		return
	}

	responses := make([]*Response, 0, len(entries))

	for _, rawEntry := range entries {
		var req Request
		if err := json.Unmarshal(rawEntry, &req); err != nil {
			responses = append(responses, s.errorResponseWithNull(InvalidRequest, "invalid request", err))
			continue
		}
		if resp := s.handleRequest(ctx, &req); resp != nil {
			responses = append(responses, resp)
		}
	}

	if len(responses) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.writeJSON(w, responses)
}

func (s *httpServer) handleRequest(ctx context.Context, req *Request) *Response {
	ctx = slogx.Context(ctx)
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

func (s *httpServer) writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *httpServer) writeError(w http.ResponseWriter, resp *Response, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if resp != nil {
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *httpServer) errorResponse(id any, code int, message string, cause error) *Response {
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

func (s *httpServer) errorResponseWithNull(code int, message string, cause error) *Response {
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
