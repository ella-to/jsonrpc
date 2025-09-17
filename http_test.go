package jsonrpc

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// MockResponseWriter implements http.ResponseWriter for testing
type MockResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func NewMockResponseWriter() *MockResponseWriter {
	return &MockResponseWriter{
		header: make(http.Header),
		status: 200,
	}
}

func (m *MockResponseWriter) Header() http.Header {
	return m.header
}

func (m *MockResponseWriter) Write(data []byte) (int, error) {
	return m.body.Write(data)
}

func (m *MockResponseWriter) WriteHeader(statusCode int) {
	m.status = statusCode
}

func (m *MockResponseWriter) GetBody() string {
	return m.body.String()
}

func (m *MockResponseWriter) GetStatus() int {
	return m.status
}

func TestNew(t *testing.T) {
	// Test with nil client
	codec := NewHttpCodec(nil)
	if codec == nil {
		t.Error("Expected codec but got nil")
		return
	}
	if codec.client == nil {
		t.Error("Expected default client to be created")
	}

	// Test with custom client
	customClient := &http.Client{}
	codec = NewHttpCodec(customClient)
	if codec.client != customClient {
		t.Error("Expected custom client to be used")
	}
}

func TestHttpNewRequest(t *testing.T) {
	w := NewMockResponseWriter()
	r := httptest.NewRequest("POST", "/rpc", nil)

	codec := NewRequestHttpCodec(w, r)
	if codec == nil {
		t.Error("Expected codec but got nil")
		return
	}
	if codec.responseWriter != w {
		t.Error("Expected response writer to be set")
	}
	if codec.request != r {
		t.Error("Expected request to be set")
	}
}

func TestHttpCodecWriteRequest(t *testing.T) {
	tests := []struct {
		name      string
		request   *Request
		wantErr   bool
		serverURL string
	}{
		{
			name: "valid request",
			request: &Request{
				JSONRPC: "2.0",
				Method:  "test.method",
				Params:  map[string]any{"arg": "value"},
				ID:      123,
			},
			wantErr:   false,
			serverURL: "http://example.com/rpc",
		},
		{
			name: "no client",
			request: &Request{
				JSONRPC: "2.0",
				Method:  "test",
				ID:      1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var codec *HttpCodec
			if tt.name == "no client" {
				codec = &HttpCodec{} // No client set
			} else {
				codec = NewHttpCodec(nil)
				codec.serverAddr = tt.serverURL
			}

			err := codec.WriteRequest(context.Background(), tt.request)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify the HTTP request was created
			if codec.request == nil {
				t.Error("Expected HTTP request to be created")
				return
			}

			if codec.request.Method != "POST" {
				t.Errorf("Expected POST method, got %s", codec.request.Method)
			}

			contentType := codec.request.Header.Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type application/json, got %s", contentType)
			}
		})
	}
}

func TestHttpCodecReadRequest(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantErr  bool
		wantType string
	}{
		{
			name:     "valid request",
			body:     `{"jsonrpc":"2.0","method":"test.method","params":{"arg":"value"},"id":123}`,
			wantErr:  false,
			wantType: "*Request",
		},
		{
			name:    "invalid JSON",
			body:    `{"invalid"}`,
			wantErr: true,
		},
		{
			name:    "no request",
			body:    ``,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "no request" {
				codec := &HttpCodec{} // No request set
				_, err := codec.ReadRequest(context.Background())
				if err == nil {
					t.Error("Expected error for no request")
				}
				return
			}

			w := NewMockResponseWriter()
			r := httptest.NewRequest("POST", "/rpc", strings.NewReader(tt.body))
			codec := NewRequestHttpCodec(w, r)

			req, err := codec.ReadRequest(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if req == nil {
				t.Error("Expected request but got nil")
				return
			}
		})
	}
}

func TestHttpCodecWriteResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *Response
		wantErr  bool
	}{
		{
			name: "valid response",
			response: &Response{
				JSONRPC: "2.0",
				Result:  "success",
				ID:      123,
			},
			wantErr: false,
		},
		{
			name: "no response writer",
			response: &Response{
				JSONRPC: "2.0",
				Result:  "test",
				ID:      1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var codec *HttpCodec
			var w *MockResponseWriter

			if tt.name == "no response writer" {
				codec = &HttpCodec{} // No response writer set
			} else {
				w = NewMockResponseWriter()
				r := httptest.NewRequest("POST", "/rpc", nil)
				codec = NewRequestHttpCodec(w, r)
			}

			err := codec.WriteResponse(context.Background(), tt.response)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if w.GetStatus() != 200 {
				t.Errorf("Expected status 200, got %d", w.GetStatus())
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type application/json, got %s", contentType)
			}

			body := w.GetBody()
			if body == "" {
				t.Error("Expected response body but got empty")
			}
		})
	}
}

func TestHttpCodecReadResponse(t *testing.T) {
	tests := []struct {
		name       string
		setupMock  func() *HttpCodec
		wantErr    bool
		wantType   string
		statusCode int
	}{
		{
			name: "valid response",
			setupMock: func() *HttpCodec {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"jsonrpc":"2.0","result":"success","id":123}`))
				}))
				codec := NewHttpCodec(&http.Client{})
				codec.serverAddr = server.URL
				// Create a request
				req, _ := http.NewRequest("POST", server.URL, strings.NewReader(`{}`))
				codec.request = req
				return codec
			},
			wantErr:    false,
			wantType:   "*Response",
			statusCode: 200,
		},
		{
			name: "server error",
			setupMock: func() *HttpCodec {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
				codec := NewHttpCodec(&http.Client{})
				codec.serverAddr = server.URL
				req, _ := http.NewRequest("POST", server.URL, strings.NewReader(`{}`))
				codec.request = req
				return codec
			},
			wantErr:    true,
			statusCode: 500,
		},
		{
			name: "no client",
			setupMock: func() *HttpCodec {
				return &HttpCodec{} // No client or request
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := tt.setupMock()

			resp, err := codec.ReadResponse(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if resp == nil {
				t.Error("Expected response but got nil")
				return
			}
		})
	}
}

func TestHttpCodecClose(t *testing.T) {
	codec := NewHttpCodec(nil)
	err := codec.Close()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestHttpCodecRemoteAddr(t *testing.T) {
	t.Run("with request", func(t *testing.T) {
		w := NewMockResponseWriter()
		r := httptest.NewRequest("POST", "/rpc", nil)
		r.RemoteAddr = "127.0.0.1:8080"
		codec := NewRequestHttpCodec(w, r)

		addr := codec.RemoteAddr()
		if addr != "127.0.0.1:8080" {
			t.Errorf("Expected 127.0.0.1:8080, got %s", addr)
		}
	})

	t.Run("with server addr", func(t *testing.T) {
		codec := NewHttpCodec(nil)
		codec.serverAddr = "http://example.com"

		addr := codec.RemoteAddr()
		if addr != "http://example.com" {
			t.Errorf("Expected http://example.com, got %s", addr)
		}
	})
}

func TestServerNewServer(t *testing.T) {
	handlers := map[string]Handler{
		"test.method": func(ctx context.Context, req any) any {
			return NewResponse("success", req.(*Request).ID)
		},
	}

	server := NewHttpServer("127.0.0.1:0", "/rpc", handlers)
	if server == nil {
		t.Error("Expected server but got nil")
		return
	}
	if server.path != "/rpc" {
		t.Errorf("Expected path /rpc, got %s", server.path)
	}
	if server.server == nil {
		t.Error("Expected HTTP server but got nil")
	}

	// Test default path
	server = NewHttpServer("127.0.0.1:0", "", handlers)
	if server.path != "/rpc" {
		t.Errorf("Expected default path /rpc, got %s", server.path)
	}
}

func TestServerHandleHTTP(t *testing.T) {
	handlers := map[string]Handler{
		"test.method": func(ctx context.Context, req any) any {
			return NewResponse("success", req.(*Request).ID)
		},
	}

	server := NewHttpServer("127.0.0.1:0", "/rpc", handlers)

	tests := []struct {
		name           string
		method         string
		body           string
		expectedStatus int
		expectJSON     bool
	}{
		{
			name:           "valid request",
			method:         "POST",
			body:           `{"jsonrpc":"2.0","method":"test.method","params":{},"id":123}`,
			expectedStatus: 200,
			expectJSON:     true,
		},
		{
			name:           "method not allowed",
			method:         "GET",
			body:           "",
			expectedStatus: 405,
			expectJSON:     false,
		},
		{
			name:           "invalid JSON",
			method:         "POST",
			body:           `{"invalid"}`,
			expectedStatus: 200,
			expectJSON:     true,
		},
		{
			name:           "method not found",
			method:         "POST",
			body:           `{"jsonrpc":"2.0","method":"unknown.method","params":{},"id":123}`,
			expectedStatus: 200,
			expectJSON:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tt.method, "/rpc", strings.NewReader(tt.body))

			server.handleHTTP(w, r)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectJSON {
				contentType := w.Header().Get("Content-Type")
				if contentType != "application/json" {
					t.Errorf("Expected Content-Type application/json, got %s", contentType)
				}
			}
		})
	}
}

func TestHttpServerUnsupportedOperations(t *testing.T) {
	server := NewHttpServer("127.0.0.1:0", "/rpc", nil)

	ctx := context.Background()

	// Accept should return error
	_, err := server.Accept(ctx)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("Expected 'not supported' error, got %v", err)
	}

	// WriteRequest should return error
	err = server.WriteRequest(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("Expected 'not supported' error, got %v", err)
	}

	// ReadRequest should return error
	_, err = server.ReadRequest(ctx)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("Expected 'not supported' error, got %v", err)
	}

	// WriteResponse should return error
	err = server.WriteResponse(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("Expected 'not supported' error, got %v", err)
	}

	// ReadResponse should return error
	_, err = server.ReadResponse(ctx)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("Expected 'not supported' error, got %v", err)
	}
}

func TestHttpServerClose(t *testing.T) {
	server := NewHttpServer("127.0.0.1:0", "/rpc", nil)

	err := server.Close()
	if err != nil {
		t.Errorf("Unexpected error on close: %v", err)
	}
}

func TestHttpServerRemoteAddr(t *testing.T) {
	server := NewHttpServer("127.0.0.1:8080", "/rpc", nil)

	addr := server.RemoteAddr()
	if addr != "127.0.0.1:8080" {
		t.Errorf("Expected 127.0.0.1:8080, got %s", addr)
	}
}

func TestClientNewClient(t *testing.T) {
	client := NewHttpClient(nil)
	if client == nil {
		t.Error("Expected client but got nil")
		return
	}
	if client.HttpCodec == nil {
		t.Error("Expected codec but got nil")
	}
	if client.IsConnected() {
		t.Error("New client should not be connected")
	}
}

func TestHttpClientConnect(t *testing.T) {
	client := NewHttpClient(nil)

	err := client.Connect(context.Background(), "http://example.com/rpc")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !client.IsConnected() {
		t.Error("Client should be connected")
	}

	if client.GetAddress() != "http://example.com/rpc" {
		t.Errorf("Expected http://example.com/rpc, got %s", client.GetAddress())
	}
}

func TestClientIsConnected(t *testing.T) {
	client := NewClient(nil)

	// Initially not connected
	if client.IsConnected() {
		t.Error("New client should not be connected")
	}

	// After connecting
	client.Connect(context.Background(), "http://example.com")
	if !client.IsConnected() {
		t.Error("Client should be connected after Connect")
	}
}

func TestClientGetAddress(t *testing.T) {
	client := NewHttpClient(nil)

	// Initially empty
	if client.GetAddress() != "" {
		t.Errorf("Expected empty address, got %s", client.GetAddress())
	}

	// After connecting
	address := "http://example.com/rpc"
	client.Connect(context.Background(), address)
	if client.GetAddress() != address {
		t.Errorf("Expected %s, got %s", address, client.GetAddress())
	}
}

func TestIntegrationHTTPClientServer(t *testing.T) {
	// Create a test handler
	handlers := map[string]Handler{
		"test.add": func(ctx context.Context, req any) any {
			request := req.(*Request)
			params, ok := request.Params.(map[string]any)
			if !ok {
				return NewErrorResponse(InvalidParams, "Invalid params", nil, request.ID)
			}

			a, ok1 := params["a"].(float64)
			b, ok2 := params["b"].(float64)
			if !ok1 || !ok2 {
				return NewErrorResponse(InvalidParams, "Parameters must be numbers", nil, request.ID)
			}

			return NewResponse(a+b, request.ID)
		},
	}

	// Start test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Create codec for this request
		codec := NewRequestHttpCodec(w, httptest.NewRequest("POST", "/rpc", bytes.NewReader(body)))
		request, err := codec.ReadRequest(r.Context())
		if err != nil {
			if rpcErr, ok := err.(*Error); ok {
				resp := NewErrorResponse(rpcErr.Code, rpcErr.Message, rpcErr.Data, nil)
				codec.WriteResponse(r.Context(), resp)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			return
		}

		// Handle request
		handler, exists := handlers[request.Method]
		if !exists {
			resp := NewErrorResponse(MethodNotFound, "Method not found", request.Method, request.ID)
			codec.WriteResponse(r.Context(), resp)
			return
		}

		resp := handler(r.Context(), request)
		if respTyped, ok := resp.(*Response); ok && !request.IsNotification() {
			codec.WriteResponse(r.Context(), respTyped)
		}
	}))
	defer server.Close()

	// Create client
	client := NewHttpClient(&http.Client{})
	client.Connect(context.Background(), server.URL)

	// Test request
	request := &Request{
		JSONRPC: "2.0",
		Method:  "test.add",
		Params:  map[string]any{"a": 5, "b": 3},
		ID:      123,
	}

	// Write and read request/response
	err := client.WriteRequest(context.Background(), request)
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	resp, err := client.ReadResponse(context.Background())
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if resp.Result != 8.0 {
		t.Errorf("Expected result 8, got %v", resp.Result)
	}

	if resp.ID.(float64) != 123 {
		t.Errorf("Expected ID 123, got %v", resp.ID)
	}
}
