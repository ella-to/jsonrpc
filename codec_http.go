// Package http provides HTTP transport implementation for JSON-RPC communication.
//
// Example Usage:
//
// Server:
//
//	handlers := map[string]Handler{
//	    "greet": func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
//	        var params map[string]any
//	        if err := json.Unmarshal(req.Params, &params); err != nil {
//	            return NewErrorResponse(jsonrpc.InvalidParams, "Invalid params", nil, req.ID)
//	        }
//	        name := params["name"].(string)
//	        return NewResponse("Hello "+name, req.ID)
//	    },
//	}
//
//	server := NewHttpServer("127.0.0.1:8080", "/rpc", handlers)
//	log.Println("Server starting on :8080/rpc")
//	if err := server.ListenAndServe(); err != nil {
//	    log.Fatal(err)
//	}
//
// Client:
//
//	client := NewHttpClient(&http.Client{})
//	if err := client.Connect(context.Background(), "http://127.0.0.1:8080/rpc"); err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	req := &Request{
//	    JSONRPC: "2.0",
//	    Method:  "greet",
//	    Params:  map[string]any{"name": "World"},
//	    ID:      1,
//	}
//
//	if err := client.WriteRequest(context.Background(), req); err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.ReadResponse(context.Background())
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Printf("Response: %v\n", resp.Result)
//
// Alternative: Using the high-level Client interface:
//
//	client := NewClient(NewHttpCodec(&http.Client{}))
//	client.Connect(context.Background(), "http://127.0.0.1:8080/rpc")
//	defer client.Close()
//
//	var result string
//	err := client.CallWithResult(context.Background(), "greet", map[string]any{"name": "World"}, &result)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Result: %s\n", result)
package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

var (
	// ErrHttpClientNotAvailable indicates the HTTP client is not available
	ErrHttpClientNotAvailable = errors.New("http: client not available")
	// ErrHttpResponseWriterNotAvailable indicates the HTTP response writer is not available
	ErrHttpResponseWriterNotAvailable = errors.New("http: response writer not available")
	// ErrHttpRequestNotAvailable indicates the HTTP request is not available
	ErrHttpRequestNotAvailable = errors.New("http: request not available")
	// ErrHttpNotSupported indicates an operation is not supported
	ErrHttpNotSupported = errors.New("http: operation not supported")
)

// Codec implements the Codec interface for HTTP connections
type HttpCodec struct {
	client     *http.Client
	serverAddr string

	// For server-side operations
	responseWriter http.ResponseWriter
	request        *http.Request

	mu sync.RWMutex
}

var _ Codec = (*HttpCodec)(nil)

// New creates a new HTTP codec for client use
func NewHttpCodec(client *http.Client) *HttpCodec {
	if client == nil {
		client = &http.Client{}
	}
	return &HttpCodec{
		client: client,
	}
}

// NewRequestHttpCodec creates a new HTTP codec for handling a single request/response
func NewRequestHttpCodec(w http.ResponseWriter, r *http.Request) *HttpCodec {
	return &HttpCodec{
		responseWriter: w,
		request:        r,
	}
}

// WriteRequest writes a JSON-RPC request via HTTP POST
func (c *HttpCodec) WriteRequest(ctx context.Context, req *Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return ErrHttpClientNotAvailable
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("http: failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.serverAddr, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("http: failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Store the request for later response reading
	c.request = httpReq

	return nil
}

// ReadRequest reads a JSON-RPC request from HTTP request body
func (c *HttpCodec) ReadRequest(ctx context.Context) (*Request, error) {
	if c.request == nil {
		return nil, ErrHttpRequestNotAvailable
	}

	body, err := io.ReadAll(c.request.Body)
	if err != nil {
		return nil, &Error{
			Code:    ParseError,
			Message: "Failed to read request body",
			Data:    err.Error(),
		}
	}
	defer c.request.Body.Close()

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, &Error{
			Code:    ParseError,
			Message: "Parse error",
			Data:    err.Error(),
		}
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	return &req, nil
}

// WriteResponse writes a JSON-RPC response via HTTP response
func (c *HttpCodec) WriteResponse(ctx context.Context, resp *Response) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.responseWriter == nil {
		return ErrHttpResponseWriterNotAvailable
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("http: failed to marshal response: %w", err)
	}

	c.responseWriter.Header().Set("Content-Type", "application/json")
	c.responseWriter.WriteHeader(http.StatusOK)

	if _, err := c.responseWriter.Write(data); err != nil {
		return fmt.Errorf("http: failed to write response: %w", err)
	}

	return nil
}

// ReadResponse reads a JSON-RPC response from HTTP response
func (c *HttpCodec) ReadResponse(ctx context.Context) (*Response, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil || c.request == nil {
		return nil, ErrHttpClientNotAvailable
	}

	httpResp, err := c.client.Do(c.request)
	if err != nil {
		return nil, fmt.Errorf("http: failed to execute HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http: server returned status %d: %s", httpResp.StatusCode, httpResp.Status)
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, &Error{
			Code:    ParseError,
			Message: "Failed to read response body",
			Data:    err.Error(),
		}
	}

	var resp Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, &Error{
			Code:    ParseError,
			Message: "Parse error",
			Data:    err.Error(),
		}
	}

	if err := resp.Validate(); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Close closes the HTTP codec (no-op for HTTP)
func (c *HttpCodec) Close() error {
	return nil
}

// RemoteAddr returns the remote address from the HTTP request
func (c *HttpCodec) RemoteAddr() string {
	if c.request != nil {
		return c.request.RemoteAddr
	}
	return c.serverAddr
}

// Handler represents a function that handles JSON-RPC requests
// We use any to avoid circular imports - this should be cast to appropriate jsonrpc
type Handler func(ctx context.Context, req *Request) *Response

// HttpServer implements Server for HTTP servers
type HttpServer struct {
	server   *http.Server
	handlers map[string]Handler
	path     string
	mu       sync.RWMutex
}

// HttpServer creates a new HTTP server codec
func NewHttpServer(address, path string, handlers map[string]Handler) *HttpServer {
	if path == "" {
		path = "/rpc"
	}

	codec := &HttpServer{
		handlers: handlers,
		path:     path,
	}

	mux := http.NewServeMux()
	mux.HandleFunc(path, codec.handleHTTP)

	codec.server = &http.Server{
		Addr:    address,
		Handler: mux,
	}

	return codec
}

// handleHTTP handles incoming HTTP requests
func (c *HttpServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	codec := NewRequestHttpCodec(w, r)

	// Read the request
	req, err := codec.ReadRequest(r.Context())
	if err != nil {
		if rpcErr, ok := err.(*Error); ok {
			resp := NewErrorResponse(rpcErr.Code, rpcErr.Message, rpcErr.Data, nil)
			codec.WriteResponse(r.Context(), resp)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// Handle the request
	c.mu.RLock()
	handler, exists := c.handlers[req.Method]
	c.mu.RUnlock()

	if !exists {
		resp := NewErrorResponse(MethodNotFound, "Method not found", req.Method, req.ID)
		codec.WriteResponse(r.Context(), resp)
		return
	}

	resp := handler(r.Context(), req)

	if resp == nil && !req.IsNotification() {
		resp = NewErrorResponse(InternalError, "Handler returned nil response", nil, req.ID)
	}

	// Don't send response for notifications
	if !req.IsNotification() && resp != nil {
		codec.WriteResponse(r.Context(), resp)
	}
}

// Accept starts the HTTP server and waits for connections
func (c *HttpServer) Accept(ctx context.Context) (Codec, error) {
	// HTTP servers don't accept individual connections like TCP
	// Instead, they handle requests via the HTTP handler
	return nil, fmt.Errorf("%w: Accept not supported for HTTP server - use ListenAndServe instead", ErrHttpNotSupported)
}

// ListenAndServe starts the HTTP server
func (c *HttpServer) ListenAndServe() error {
	return c.server.ListenAndServe()
}

// ListenAndServeContext starts the HTTP server with context
func (c *HttpServer) ListenAndServeContext(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		c.server.Shutdown(context.Background())
	}()

	err := c.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Close closes the HTTP server
func (c *HttpServer) Close() error {
	return c.server.Shutdown(context.Background())
}

// RemoteAddr returns the server address
func (c *HttpServer) RemoteAddr() string {
	return c.server.Addr
}

// WriteRequest, ReadRequest, WriteResponse, ReadResponse are not used in server mode
func (c *HttpServer) WriteRequest(ctx context.Context, req any) error {
	return fmt.Errorf("%w: WriteRequest not supported on HTTP server", ErrHttpNotSupported)
}

func (c *HttpServer) ReadRequest(ctx context.Context) (any, error) {
	return nil, fmt.Errorf("%w: ReadRequest not supported on HTTP server", ErrHttpNotSupported)
}

func (c *HttpServer) WriteResponse(ctx context.Context, resp any) error {
	return fmt.Errorf("%w: WriteResponse not supported on HTTP server", ErrHttpNotSupported)
}

func (c *HttpServer) ReadResponse(ctx context.Context) (any, error) {
	return nil, fmt.Errorf("%w: ReadResponse not supported on HTTP server", ErrHttpNotSupported)
}

// Client implements Client for HTTP clients
type HttpClient struct {
	*HttpCodec
	address string
	mu      sync.RWMutex
}

// NewClient creates a new HTTP client codec
func NewHttpClient(client *http.Client) *HttpClient {
	return &HttpClient{
		HttpCodec: NewHttpCodec(client),
	}
}

// Connect sets the server address for HTTP requests
func (c *HttpClient) Connect(ctx context.Context, address string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.serverAddr = address
	c.address = address
	return nil
}

// IsConnected returns true if the client has an address set
func (c *HttpClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.address != ""
}

// GetAddress returns the connected address
func (c *HttpClient) GetAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.address
}
