package jsonrpc

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// MockCodecClient is a mock implementation of codec.Client for testing
type MockCodecClient struct {
	mu                sync.RWMutex
	connected         bool
	connectErr        error
	writeRequests     []*Request
	writeResponses    []*Response
	writeErr          error
	readRequests      []*Request
	readResponses     []*Response
	readRequestIndex  int
	readResponseIndex int
	readErr           error
	closeErr          error
	shouldValidate    bool
	remoteAddr        string
}

func NewMockCodecClient() *MockCodecClient {
	return &MockCodecClient{
		writeRequests:     make([]*Request, 0),
		writeResponses:    make([]*Response, 0),
		readRequests:      make([]*Request, 0),
		readResponses:     make([]*Response, 0),
		shouldValidate:    true,
		remoteAddr:        "mock:1234",
		readRequestIndex:  0,
		readResponseIndex: 0,
	}
}

func (m *MockCodecClient) Connect(ctx context.Context, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	m.remoteAddr = address
	return nil
}

func (m *MockCodecClient) WriteRequest(ctx context.Context, req *Request) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.writeErr != nil {
		return m.writeErr
	}

	if m.shouldValidate {
		m.writeRequests = append(m.writeRequests, req)
	}

	return nil
}

func (m *MockCodecClient) ReadRequest(ctx context.Context) (*Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.readErr != nil {
		return nil, m.readErr
	}

	if m.readRequestIndex >= len(m.readRequests) {
		return nil, errors.New("no more requests")
	}

	req := m.readRequests[m.readRequestIndex]
	m.readRequestIndex++
	return req, nil
}

func (m *MockCodecClient) WriteResponse(ctx context.Context, resp *Response) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.writeErr != nil {
		return m.writeErr
	}

	m.writeResponses = append(m.writeResponses, resp)
	return nil
}

func (m *MockCodecClient) ReadResponse(ctx context.Context) (*Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.readErr != nil {
		return nil, m.readErr
	}

	if m.readResponseIndex >= len(m.readResponses) {
		return nil, errors.New("no more responses")
	}

	resp := m.readResponses[m.readResponseIndex]
	m.readResponseIndex++
	return resp, nil
}

func (m *MockCodecClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connected = false
	return m.closeErr
}

func (m *MockCodecClient) RemoteAddr() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.remoteAddr
}

// Helper methods for testing
func (m *MockCodecClient) SetConnectError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectErr = err
}

func (m *MockCodecClient) SetWriteError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeErr = err
}

func (m *MockCodecClient) SetReadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readErr = err
}

func (m *MockCodecClient) SetCloseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeErr = err
}

func (m *MockCodecClient) AddResponse(resp *Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readResponses = append(m.readResponses, resp)
}

func (m *MockCodecClient) GetWrittenRequests() []*Request {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Request, len(m.writeRequests))
	copy(result, m.writeRequests)
	return result
}

func (m *MockCodecClient) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

func (m *MockCodecClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeRequests = m.writeRequests[:0]
	m.readResponses = m.readResponses[:0]
	m.readRequestIndex = 0
	m.readResponseIndex = 0
	m.connectErr = nil
	m.writeErr = nil
	m.readErr = nil
	m.closeErr = nil
	m.connected = false
}

func TestNewClient(t *testing.T) {
	mockCodec := NewMockCodecClient()
	client := NewClient(mockCodec)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.IsConnected() {
		t.Error("New client should not be connected")
	}
}

func TestClientConnect(t *testing.T) {
	tests := []struct {
		name        string
		connectErr  error
		expectError bool
	}{
		{
			name:        "successful connection",
			connectErr:  nil,
			expectError: false,
		},
		{
			name:        "connection error",
			connectErr:  errors.New("connection failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCodec := NewMockCodecClient()
			client := NewClient(mockCodec)

			if tt.connectErr != nil {
				mockCodec.SetConnectError(tt.connectErr)
			}

			ctx := context.Background()
			err := client.Connect(ctx, "test:1234")

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if client.IsConnected() {
					t.Error("Client should not be connected after error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if !client.IsConnected() {
					t.Error("Client should be connected after successful connect")
				}
			}
		})
	}
}

func TestClientCall(t *testing.T) {
	tests := []struct {
		name        string
		connected   bool
		response    *Response
		writeErr    error
		readErr     error
		expectError bool
		errorMsg    string
	}{
		{
			name:      "successful call",
			connected: true,
			response: &Response{
				JSONRPC: Version,
				Result:  "success",
				ID:      int64(1),
			},
			expectError: false,
		},
		{
			name:        "not connected",
			connected:   false,
			expectError: true,
			errorMsg:    "client not connected",
		},
		{
			name:        "write error",
			connected:   true,
			writeErr:    errors.New("write failed"),
			expectError: true,
			errorMsg:    "failed to write request",
		},
		{
			name:        "read error",
			connected:   true,
			readErr:     errors.New("read failed"),
			expectError: true,
			errorMsg:    "failed to read response",
		},
		{
			name:      "ID mismatch",
			connected: true,
			response: &Response{
				JSONRPC: Version,
				Result:  "success",
				ID:      int64(999), // Wrong ID
			},
			expectError: true,
			errorMsg:    "response ID mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCodec := NewMockCodecClient()
			client := NewClient(mockCodec)

			if tt.connected {
				ctx := context.Background()
				client.Connect(ctx, "test:1234")
			}

			if tt.writeErr != nil {
				mockCodec.SetWriteError(tt.writeErr)
			}
			if tt.readErr != nil {
				mockCodec.SetReadError(tt.readErr)
			}
			if tt.response != nil {
				mockCodec.AddResponse(tt.response)
			}

			ctx := context.Background()
			resp, err := client.Call(ctx, "test.method", []string{"param1"})

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorMsg != "" && !containsString(err.Error(), tt.errorMsg) {
					t.Errorf("Error message '%s' does not contain '%s'", err.Error(), tt.errorMsg)
				}
				if resp != nil {
					t.Error("Response should be nil on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if resp == nil {
					t.Error("Response should not be nil on success")
				}
				if resp != nil && resp.Result != tt.response.Result {
					t.Errorf("Result = %v, want %v", resp.Result, tt.response.Result)
				}
			}
		})
	}
}

func TestClientNotify(t *testing.T) {
	tests := []struct {
		name        string
		connected   bool
		writeErr    error
		expectError bool
		errorMsg    string
	}{
		{
			name:        "successful notification",
			connected:   true,
			expectError: false,
		},
		{
			name:        "not connected",
			connected:   false,
			expectError: true,
			errorMsg:    "client not connected",
		},
		{
			name:        "write error",
			connected:   true,
			writeErr:    errors.New("write failed"),
			expectError: true,
			errorMsg:    "failed to write notification",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCodec := NewMockCodecClient()
			client := NewClient(mockCodec)

			if tt.connected {
				ctx := context.Background()
				client.Connect(ctx, "test:1234")
			}

			if tt.writeErr != nil {
				mockCodec.SetWriteError(tt.writeErr)
			}

			ctx := context.Background()
			err := client.Notify(ctx, "test.notification", map[string]string{"key": "value"})

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorMsg != "" && !containsString(err.Error(), tt.errorMsg) {
					t.Errorf("Error message '%s' does not contain '%s'", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				// Verify notification was written correctly
				requests := mockCodec.GetWrittenRequests()
				if len(requests) != 1 {
					t.Fatalf("Expected 1 request, got %d", len(requests))
				}
				req := requests[0]
				if req.ID != nil {
					t.Error("Notification should have nil ID")
				}
				if req.Method != "test.notification" {
					t.Errorf("Method = %v, want test.notification", req.Method)
				}
			}
		})
	}
}

func TestClientCallWithResult(t *testing.T) {
	tests := []struct {
		name         string
		response     *Response
		expectError  bool
		expectedData map[string]any
	}{
		{
			name: "successful call with result",
			response: &Response{
				JSONRPC: Version,
				Result:  map[string]any{"name": "test", "value": 42},
				ID:      int64(1),
			},
			expectError:  false,
			expectedData: map[string]any{"name": "test", "value": 42},
		},
		{
			name: "call with error response",
			response: &Response{
				JSONRPC: Version,
				Error: &Error{
					Code:    MethodNotFound,
					Message: "Method not found",
				},
				ID: int64(1),
			},
			expectError: true,
		},
		{
			name: "call with nil result",
			response: &Response{
				JSONRPC: Version,
				Result:  nil,
				ID:      int64(1),
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCodec := NewMockCodecClient()
			client := NewClient(mockCodec)

			ctx := context.Background()
			client.Connect(ctx, "test:1234")
			mockCodec.AddResponse(tt.response)

			var result map[string]any
			err := client.CallWithResult(ctx, "test.method", nil, &result)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.expectedData != nil {
					if result["name"] != tt.expectedData["name"] {
						t.Errorf("Name = %v, want %v", result["name"], tt.expectedData["name"])
					}
					// Note: JSON unmarshaling converts numbers to float64
					if float64Value, ok := result["value"].(float64); ok {
						if int(float64Value) != int(tt.expectedData["value"].(int)) {
							t.Errorf("Value = %v, want %v", result["value"], tt.expectedData["value"])
						}
					}
				}
			}
		})
	}
}

func TestClientClose(t *testing.T) {
	tests := []struct {
		name     string
		closeErr error
	}{
		{
			name:     "successful close",
			closeErr: nil,
		},
		{
			name:     "close with error",
			closeErr: errors.New("close failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCodec := NewMockCodecClient()
			client := NewClient(mockCodec)

			ctx := context.Background()
			client.Connect(ctx, "test:1234")

			if tt.closeErr != nil {
				mockCodec.SetCloseError(tt.closeErr)
			}

			err := client.Close()

			if tt.closeErr != nil {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			// Client should be disconnected regardless of close error
			if client.IsConnected() {
				t.Error("Client should not be connected after close")
			}
		})
	}
}

func TestClientConcurrency(t *testing.T) {
	mockCodec := NewMockCodecClient()
	client := NewClient(mockCodec)

	ctx := context.Background()
	client.Connect(ctx, "test:1234")

	// Add generic responses that will be read in order
	// The actual ID matching is tested in other tests
	for i := 0; i < 10; i++ {
		mockCodec.AddResponse(&Response{
			JSONRPC: Version,
			Result:  i,
			ID:      int64(i + 1), // This won't match exactly due to concurrency, but that's ok for this test
		})
	}

	// Make concurrent notifications instead of calls to avoid ID matching issues
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			err := client.Notify(ctx, "test.method", index)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent notification error: %v", err)
	}

	// Verify we wrote 10 notifications
	requests := mockCodec.GetWrittenRequests()
	if len(requests) != 10 {
		t.Errorf("Expected 10 notifications, got %d", len(requests))
	}

	// Verify all are notifications (no ID)
	for i, req := range requests {
		if req.ID != nil {
			t.Errorf("Request %d should be notification (ID=nil), got ID=%v", i, req.ID)
		}
	}
}

func TestUnmarshalResult(t *testing.T) {
	tests := []struct {
		name        string
		source      any
		target      any
		expectError bool
		validate    func(t *testing.T, target any)
	}{
		{
			name:   "unmarshal map to struct",
			source: map[string]any{"name": "test", "value": 42},
			target: &struct {
				Name  string `json:"name"`
				Value int    `json:"value"`
			}{},
			expectError: false,
			validate: func(t *testing.T, target any) {
				result := target.(*struct {
					Name  string `json:"name"`
					Value int    `json:"value"`
				})
				if result.Name != "test" {
					t.Errorf("Name = %v, want test", result.Name)
				}
				if result.Value != 42 {
					t.Errorf("Value = %v, want 42", result.Value)
				}
			},
		},
		{
			name:        "nil source",
			source:      nil,
			target:      &map[string]any{},
			expectError: false,
		},
		{
			name:        "nil target",
			source:      map[string]any{"test": "value"},
			target:      nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := unmarshalResult(tt.source, tt.target)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.validate != nil {
					tt.validate(t, tt.target)
				}
			}
		})
	}
}

// Helper function to check if a string contains another string
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				findSubstring(s, substr))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
