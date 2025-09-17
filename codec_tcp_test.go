package jsonrpc

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// MockConn implements net.Conn for testing
type MockConn struct {
	readData  []byte
	readPos   int
	writeData strings.Builder
	closed    bool
}

func NewMockConn(readData string) *MockConn {
	return &MockConn{readData: []byte(readData)}
}

func (m *MockConn) Read(b []byte) (n int, err error) {
	if m.closed {
		return 0, ErrTcpConnectionClosed
	}
	if m.readPos >= len(m.readData) {
		return 0, ErrTcpConnectionClosed
	}
	n = copy(b, m.readData[m.readPos:])
	m.readPos += n
	return n, nil
}

func (m *MockConn) Write(b []byte) (n int, err error) {
	if m.closed {
		return 0, ErrTcpConnectionClosed
	}
	return m.writeData.Write(b)
}

func (m *MockConn) Close() error {
	m.closed = true
	return nil
}

func (m *MockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

func (m *MockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8081}
}

func (m *MockConn) SetDeadline(t time.Time) error      { return nil }
func (m *MockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *MockConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *MockConn) GetWrittenData() string {
	return m.writeData.String()
}

func TestCodecNew(t *testing.T) {
	conn := NewMockConn("")
	codec := NewTcpCodec(conn)

	if codec.conn != conn {
		t.Error("Codec should store the connection")
	}
	if codec.reader == nil {
		t.Error("Codec should initialize reader")
	}
	if codec.writer == nil {
		t.Error("Codec should initialize writer")
	}
}

func TestCodecWriteRequest(t *testing.T) {
	tests := []struct {
		name     string
		request  *Request
		wantErr  bool
		expected string
	}{
		{
			name: "valid request",
			request: &Request{
				JSONRPC: "2.0",
				Method:  "test.method",
				Params:  map[string]any{"arg": "value"},
				ID:      123,
			},
			wantErr:  false,
			expected: "74\n{\"jsonrpc\":\"2.0\",\"method\":\"test.method\",\"params\":{\"arg\":\"value\"},\"id\":123}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewMockConn("")
			codec := NewTcpCodec(conn)

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

			written := conn.GetWrittenData()
			if written != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, written)
			}
		})
	}
}

func TestCodecReadRequest(t *testing.T) {
	tests := []struct {
		name     string
		readData string
		wantErr  bool
		wantType string
	}{
		{
			name:     "valid request",
			readData: "74\n{\"jsonrpc\":\"2.0\",\"method\":\"test.method\",\"params\":{\"arg\":\"value\"},\"id\":123}",
			wantErr:  false,
			wantType: "*Request",
		},
		{
			name:     "invalid JSON",
			readData: "10\n{\"invalid\"}",
			wantErr:  true,
		},
		{
			name:     "invalid length prefix",
			readData: "abc\n{\"jsonrpc\":\"2.0\"}",
			wantErr:  true,
		},
		{
			name:     "empty connection",
			readData: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewMockConn(tt.readData)
			codec := NewTcpCodec(conn)

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

func TestCodecWriteResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *Response
		wantErr  bool
		expected string
	}{
		{
			name: "valid response",
			response: &Response{
				JSONRPC: "2.0",
				Result:  "success",
				ID:      123,
			},
			wantErr:  false,
			expected: "45\n{\"jsonrpc\":\"2.0\",\"result\":\"success\",\"id\":123}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewMockConn("")
			codec := NewTcpCodec(conn)

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

			written := conn.GetWrittenData()
			if written != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, written)
			}
		})
	}
}

func TestCodecReadResponse(t *testing.T) {
	tests := []struct {
		name     string
		readData string
		wantErr  bool
		wantType string
	}{
		{
			name:     "valid response",
			readData: "45\n{\"jsonrpc\":\"2.0\",\"result\":\"success\",\"id\":123}",
			wantErr:  false,
			wantType: "*Response",
		},
		{
			name:     "invalid JSON",
			readData: "10\n{\"invalid\"}",
			wantErr:  true,
		},
		{
			name:     "invalid length prefix",
			readData: "xyz\n{\"jsonrpc\":\"2.0\"}",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewMockConn(tt.readData)
			codec := NewTcpCodec(conn)

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

func TestCodecClose(t *testing.T) {
	t.Run("successful close", func(t *testing.T) {
		conn := NewMockConn("")
		codec := NewTcpCodec(conn)

		err := codec.Close()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if !conn.closed {
			t.Error("Connection should be closed")
		}
	})

	t.Run("already closed", func(t *testing.T) {
		codec := &TcpCodec{conn: nil}

		err := codec.Close()
		if err != ErrTcpConnectionClosed {
			t.Errorf("Expected ErrTcpConnectionClosed, got %v", err)
		}
	})
}

func TestCodecRemoteAddr(t *testing.T) {
	t.Run("with connection", func(t *testing.T) {
		conn := NewMockConn("")
		codec := NewTcpCodec(conn)

		addr := codec.RemoteAddr()
		expected := "127.0.0.1:8081"
		if addr != expected {
			t.Errorf("Expected %q, got %q", expected, addr)
		}
	})

	t.Run("without connection", func(t *testing.T) {
		codec := &TcpCodec{conn: nil}

		addr := codec.RemoteAddr()
		if addr != "" {
			t.Errorf("Expected empty string, got %q", addr)
		}
	})
}

func TestCodecLengthLimits(t *testing.T) {
	tests := []struct {
		name     string
		readData string
		wantErr  bool
	}{
		{
			name:     "negative length",
			readData: "-1\n",
			wantErr:  true,
		},
		{
			name:     "too large length",
			readData: "2000000\n", // 2MB > 1MB limit
			wantErr:  true,
		},
		{
			name:     "valid length",
			readData: "5\nhello",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewMockConn(tt.readData)
			codec := NewTcpCodec(conn)

			_, err := codec.readMessage()

			if tt.wantErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestServerNewTcpServer(t *testing.T) {
	// Test with an available port
	server, err := NewTcpServer("127.0.0.1:0")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if server == nil {
		t.Error("Expected server but got nil")
		return
	}
	if server.listener == nil {
		t.Error("Expected listener but got nil")
	}

	// Clean up
	server.Close()

	// Test with invalid address
	_, err = NewTcpServer("invalid-address")
	if err == nil {
		t.Error("Expected error for invalid address")
	}
}

func TestServerClose(t *testing.T) {
	server, err := NewTcpServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// First close should succeed
	err = server.Close()
	if err != nil {
		t.Errorf("Unexpected error on close: %v", err)
	}

	// Second close should return error
	err = server.Close()
	if err != ErrTcpConnectionClosed {
		t.Errorf("Expected ErrTcpConnectionClosed, got %v", err)
	}
}

func TestServerRemoteAddr(t *testing.T) {
	server, err := NewTcpServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	addr := server.RemoteAddr()
	if addr == "" {
		t.Error("Expected non-empty address")
	}

	// Close and check again
	server.Close()
	addr = server.RemoteAddr()
	if addr != "" {
		t.Errorf("Expected empty address after close, got %q", addr)
	}
}

func TestServerUnsupportedOperations(t *testing.T) {
	server, err := NewTcpServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	ctx := context.Background()

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

func TestTcpClientNewClient(t *testing.T) {
	client := NewTcpClient()
	if client == nil {
		t.Error("Expected client but got nil")
	}
	if client.IsConnected() {
		t.Error("New client should not be connected")
	}
}

func TestTcpClientConnect(t *testing.T) {
	// Start a test server
	server, err := NewTcpServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	addr := server.RemoteAddr()
	client := NewTcpClient()

	// Test successful connection
	ctx := context.Background()
	err = client.Connect(ctx, addr)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !client.IsConnected() {
		t.Error("Client should be connected")
	}

	if client.GetAddress() != addr {
		t.Errorf("Expected address %q, got %q", addr, client.GetAddress())
	}

	// Clean up
	client.Close()

	// Test connection to invalid address
	err = client.Connect(ctx, "invalid-address")
	if err == nil {
		t.Error("Expected error for invalid address")
	}
}

func TestTcpClientIsConnected(t *testing.T) {
	client := NewTcpClient()

	// Initially not connected
	if client.IsConnected() {
		t.Error("New client should not be connected")
	}

	// After setting a mock connection
	client.TcpCodec = NewTcpCodec(NewMockConn(""))
	if !client.IsConnected() {
		t.Error("Client with codec should be connected")
	}

	// After closing
	client.Close()
	// Note: IsConnected checks if Codec and conn are not nil,
	// but Close() sets conn to closed state, not nil
	// So we need to manually set it to test this case
	client.TcpCodec = nil
	if client.IsConnected() {
		t.Error("Client without codec should not be connected")
	}
}

func TestCodecConcurrency(t *testing.T) {
	// Test that codec operations are thread-safe
	conn := NewMockConn("24\n{\"jsonrpc\":\"2.0\",\"id\":1}")
	codec := NewTcpCodec(conn)

	done := make(chan bool, 2)

	// Concurrent reads
	go func() {
		codec.readMessage()
		done <- true
	}()

	// Concurrent writes
	go func() {
		codec.writeMessage(map[string]any{"test": "data"})
		done <- true
	}()

	// Wait for both operations to complete
	<-done
	<-done

	// No assertions needed - we're just testing that there are no race conditions
}

func TestMessageFraming(t *testing.T) {
	// Test the length-prefix message framing protocol
	conn := NewMockConn("")
	codec := NewTcpCodec(conn)

	// Test message with exact length
	testData := map[string]any{
		"jsonrpc": "2.0",
		"method":  "test",
		"id":      1,
	}

	err := codec.writeMessage(testData)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	written := conn.GetWrittenData()

	// Parse the written data to verify framing
	parts := strings.SplitN(written, "\n", 2)
	if len(parts) != 2 {
		t.Errorf("Expected 2 parts (length + data), got %d", len(parts))
		return
	}

	// Verify that the length prefix matches the actual data length
	expectedLength := len(parts[1])
	actualLength := parts[0]

	if actualLength != "40" { // JSON marshaled length for the test data
		t.Errorf("Expected length prefix to be '40', got %s for %d bytes", actualLength, expectedLength)
	}
}
