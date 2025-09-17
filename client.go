package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

// Client represents a JSON-RPC client that can make requests using any codec
type Client struct {
	codec     ClientCodec
	nextID    int64
	mu        sync.RWMutex
	connected bool
}

// NewClient creates a new JSON-RPC client
func NewClient(codec ClientCodec) *Client {
	return &Client{
		codec: codec,
	}
}

// Connect connects the client to the specified address
func (c *Client) Connect(ctx context.Context, address string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.codec.Connect(ctx, address); err != nil {
		return err
	}

	c.connected = true
	return nil
}

// Call makes a synchronous JSON-RPC call
func (c *Client) Call(ctx context.Context, method string, params any) (*Response, error) {
	if !c.connected {
		return nil, fmt.Errorf("client not connected")
	}

	// Generate unique request ID
	id := atomic.AddInt64(&c.nextID, 1)

	// Create request
	req := NewRequest(method, params, id)

	// Send request
	if err := c.codec.WriteRequest(ctx, req); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read response
	resp, err := c.codec.ReadResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Verify response ID matches request ID
	// Handle type conversion from JSON unmarshaling (int64 -> float64)
	var expectedID any = id
	if respIDFloat, ok := resp.ID.(float64); ok {
		expectedID = float64(id)
		if respIDFloat != expectedID {
			return nil, fmt.Errorf("response ID mismatch: expected %v, got %v", expectedID, resp.ID)
		}
	} else if resp.ID != id {
		return nil, fmt.Errorf("response ID mismatch: expected %v, got %v", id, resp.ID)
	}

	// Check for error response
	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp, nil
}

// Notify sends a notification (request without expecting a response)
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	if !c.connected {
		return fmt.Errorf("client not connected")
	}

	// Create notification (no ID)
	req := NewNotification(method, params)

	// Send notification
	if err := c.codec.WriteRequest(ctx, req); err != nil {
		return fmt.Errorf("failed to write notification: %w", err)
	}

	return nil
}

// CallWithResult makes a call and unmarshals the result into the provided variable
func (c *Client) CallWithResult(ctx context.Context, method string, params any, result any) error {
	resp, err := c.Call(ctx, method, params)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return resp.Error
	}

	if result != nil && resp.Result != nil {
		// If result is provided, unmarshal the response result
		return unmarshalResult(resp.Result, result)
	}

	return nil
}

// BatchCall makes multiple calls in a single batch (for HTTP transport)
func (c *Client) BatchCall(ctx context.Context, calls []BatchCall) ([]BatchResponse, error) {
	if !c.connected {
		return nil, fmt.Errorf("client not connected")
	}

	// Create batch request
	requests := make([]*Request, len(calls))
	for i, call := range calls {
		id := atomic.AddInt64(&c.nextID, 1)
		requests[i] = NewRequest(call.Method, call.Params, id)
	}

	// For now, send individual requests (batch support can be added later)
	responses := make([]BatchResponse, len(calls))
	for i, req := range requests {
		resp, err := c.sendSingleRequest(ctx, req)
		responses[i] = BatchResponse{
			Response: resp,
			Error:    err,
			Index:    i,
		}
	}

	return responses, nil
}

// sendSingleRequest sends a single request and returns the response
func (c *Client) sendSingleRequest(ctx context.Context, req *Request) (*Response, error) {
	if err := c.codec.WriteRequest(ctx, req); err != nil {
		return nil, err
	}

	return c.codec.ReadResponse(ctx)
}

// Close closes the client connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	return c.codec.Close()
}

// IsConnected returns true if the client is connected
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// BatchCall represents a single call in a batch
type BatchCall struct {
	Method string
	Params any
}

// BatchResponse represents a response in a batch
type BatchResponse struct {
	Response *Response
	Error    error
	Index    int
}

// NewTCPClient creates a new TCP JSON-RPC client
func NewTCPClient() *Client {
	// This will be implemented using a factory pattern
	panic("NewTCPClient: use codec/tcp.NewClient() and create client with NewClient()")
}

// NewHTTPClient creates a new HTTP JSON-RPC client
func NewHTTPClient(httpClient *http.Client) *Client {
	// This will be implemented using a factory pattern
	panic("NewHTTPClient: use codec/http.NewClient() and create client with NewClient()")
}

// SimpleClient provides a simple way to create clients
func NewSimpleClient(transport string) (*Client, error) {
	switch transport {
	case "tcp":
		return NewTCPClient(), nil
	case "http":
		return NewHTTPClient(nil), nil
	default:
		return nil, fmt.Errorf("unsupported transport: %s", transport)
	}
}

// unmarshalResult unmarshals a JSON result into the target variable
func unmarshalResult(source, target any) error {
	if source == nil {
		return nil
	}

	if target == nil {
		return fmt.Errorf("target cannot be nil")
	}

	// Convert source to JSON bytes then unmarshal to target
	// This handles the case where source is map[string]any from JSON
	jsonBytes, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("failed to marshal source: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, target); err != nil {
		return fmt.Errorf("failed to unmarshal into target: %w", err)
	}

	return nil
}
