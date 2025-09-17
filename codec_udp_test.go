package jsonrpc

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewUdpCodec(t *testing.T) {
	// Create a UDP connection for testing
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	remoteAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}

	// Test basic constructor
	codec := NewUdpCodec(conn, remoteAddr)
	if codec == nil {
		t.Error("Expected codec but got nil")
		return
	}
	if codec.conn != conn {
		t.Error("Expected connection to be set")
	}
	if codec.remoteAddr != remoteAddr {
		t.Error("Expected remote address to be set")
	}
	if codec.timeout != DefaultTimeout {
		t.Errorf("Expected default timeout %v, got %v", DefaultTimeout, codec.timeout)
	}

	// Test constructor with timeout
	customTimeout := 10 * time.Second
	codecWithTimeout := NewUdpCodecWithTimeout(conn, remoteAddr, customTimeout)
	if codecWithTimeout.timeout != customTimeout {
		t.Errorf("Expected custom timeout %v, got %v", customTimeout, codecWithTimeout.timeout)
	}
}

func TestUdpCodecClose(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create UDP connection: %v", err)
	}

	codec := NewUdpCodec(conn, nil)

	// Test normal close
	err = codec.Close()
	if err != nil {
		t.Errorf("Unexpected error on close: %v", err)
	}

	// Test closing already closed codec
	err = codec.Close()
	if err != ErrUdpConnectionClosed {
		t.Errorf("Expected ErrUdpConnectionClosed, got %v", err)
	}
}

func TestUdpCodecRemoteAddr(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	t.Run("with remote address", func(t *testing.T) {
		remoteAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
		codec := NewUdpCodec(conn, remoteAddr)

		addr := codec.RemoteAddr()
		if addr != "127.0.0.1:8080" {
			t.Errorf("Expected 127.0.0.1:8080, got %s", addr)
		}
	})

	t.Run("without remote address", func(t *testing.T) {
		codec := NewUdpCodec(conn, nil)

		addr := codec.RemoteAddr()
		if addr != "" {
			t.Errorf("Expected empty string, got %s", addr)
		}
	})
}

func TestUdpCodecLocalAddr(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	codec := NewUdpCodec(conn, nil)
	addr := codec.LocalAddr()

	if addr == "" {
		t.Error("Expected local address but got empty string")
	}
	if !strings.Contains(addr, "127.0.0.1") {
		t.Errorf("Expected address to contain 127.0.0.1, got %s", addr)
	}
}

func TestUdpCodecSetTimeout(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	codec := NewUdpCodec(conn, nil)
	newTimeout := 5 * time.Second

	codec.SetTimeout(newTimeout)
	if codec.timeout != newTimeout {
		t.Errorf("Expected timeout %v, got %v", newTimeout, codec.timeout)
	}
}

func TestUdpCodecWriteRequest(t *testing.T) {
	tests := []struct {
		name       string
		request    *Request
		wantErr    bool
		setup      func() *UdpCodec
		errorCheck func(error) bool
	}{
		{
			name: "valid request",
			request: &Request{
				JSONRPC: "2.0",
				Method:  "test.method",
				Params:  map[string]any{"arg": "value"},
				ID:      123,
			},
			wantErr: false,
			setup: func() *UdpCodec {
				// Create two connected UDP connections
				addr1, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
				conn1, _ := net.ListenUDP("udp", addr1)

				addr2, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
				conn2, _ := net.ListenUDP("udp", addr2)

				// Create codec with conn1 connecting to conn2
				codec := NewUdpCodec(conn1, conn2.LocalAddr().(*net.UDPAddr))

				// Clean up conn2 after test
				go func() {
					time.Sleep(100 * time.Millisecond)
					conn2.Close()
				}()

				return codec
			},
		},
		{
			name: "no remote address and not connected",
			request: &Request{
				JSONRPC: "2.0",
				Method:  "test",
				ID:      1,
			},
			wantErr: true,
			setup: func() *UdpCodec {
				addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
				conn, _ := net.ListenUDP("udp", addr)
				return NewUdpCodec(conn, nil)
			},
			errorCheck: func(err error) bool {
				return strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "write")
			},
		},
		{
			name: "closed connection",
			request: &Request{
				JSONRPC: "2.0",
				Method:  "test",
				ID:      1,
			},
			wantErr: true,
			setup: func() *UdpCodec {
				conn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
				codec := NewUdpCodec(conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				codec.Close()
				return codec
			},
			errorCheck: func(err error) bool {
				return err == ErrUdpConnectionClosed
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := tt.setup()
			defer codec.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			err := codec.WriteRequest(ctx, tt.request)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if tt.errorCheck != nil && !tt.errorCheck(err) {
					t.Errorf("Error check failed for error: %v", err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestUdpCodecMessageTooLarge(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	codec := NewUdpCodec(conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	// Create a request with very large params
	largeData := strings.Repeat("x", MaxUDPPacketSize)
	request := &Request{
		JSONRPC: "2.0",
		Method:  "test.method",
		Params:  map[string]any{"data": largeData},
		ID:      123,
	}

	ctx := context.Background()
	err = codec.WriteRequest(ctx, request)

	if err == nil {
		t.Error("Expected error for large message but got none")
		return
	}

	if !strings.Contains(err.Error(), "message too large") {
		t.Errorf("Expected 'message too large' error, got %v", err)
	}
}

func TestUdpCodecIntegration(t *testing.T) {
	// Create server
	server, err := NewUdpServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	serverAddr := server.RemoteAddr()

	// Test request/response cycle using two separate UDP connections
	// to simulate realistic client-server communication

	// Server side: listen for incoming messages
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Accept connection (get first message)
		codec, acceptErr := server.Accept(ctx)
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer codec.Close()

		// Read request
		req, readErr := codec.ReadRequest(ctx)
		if readErr != nil {
			serverDone <- readErr
			return
		}

		// Verify request
		if req.Method != "test.echo" {
			serverDone <- readErr
			return
		}

		// Send response
		response := &Response{
			JSONRPC: "2.0",
			Result:  "echo: hello",
			ID:      req.ID,
		}

		writeErr := codec.WriteResponse(ctx, response)
		serverDone <- writeErr
	}()

	// Give server time to start accepting
	time.Sleep(100 * time.Millisecond)

	// Client side: send request and read response
	// Create a simple UDP connection to send data to server
	serverUDPAddr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		t.Fatalf("Failed to resolve server address: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, serverUDPAddr)
	if err != nil {
		t.Fatalf("Failed to create client connection: %v", err)
	}
	defer conn.Close()

	// Create client codec with connected socket (no remote addr needed for writes)
	clientCodec := NewUdpCodec(conn, nil)
	defer clientCodec.Close()

	// Test request/response cycle
	request := &Request{
		JSONRPC: "2.0",
		Method:  "test.echo",
		Params:  map[string]any{"message": "hello"},
		ID:      42,
	}

	// Client sends request
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = clientCodec.WriteRequest(ctx, request)
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Client reads response
	resp, err := clientCodec.ReadResponse(ctx)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Verify response
	if resp.Result != "echo: hello" {
		t.Errorf("Expected result 'echo: hello', got %v", resp.Result)
	}

	if resp.ID.(float64) != 42 {
		t.Errorf("Expected ID 42, got %v", resp.ID)
	}

	// Wait for server to complete
	if err := <-serverDone; err != nil {
		t.Errorf("Server error: %v", err)
	}
}

func TestUdpServer(t *testing.T) {
	t.Run("create server", func(t *testing.T) {
		server, err := NewUdpServer("127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		defer server.Close()

		if server == nil {
			t.Error("Expected server but got nil")
			return
		}

		addr := server.RemoteAddr()
		if addr == "" {
			t.Error("Expected server address but got empty string")
		}
		if !strings.Contains(addr, "127.0.0.1") {
			t.Errorf("Expected address to contain 127.0.0.1, got %s", addr)
		}
	})

	t.Run("create server with timeout", func(t *testing.T) {
		customTimeout := 5 * time.Second
		server, err := NewUdpServerWithTimeout("127.0.0.1:0", customTimeout)
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		defer server.Close()

		if server.timeout != customTimeout {
			t.Errorf("Expected timeout %v, got %v", customTimeout, server.timeout)
		}
	})

	t.Run("server set timeout", func(t *testing.T) {
		server, err := NewUdpServer("127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		defer server.Close()

		newTimeout := 3 * time.Second
		server.SetTimeout(newTimeout)
		if server.timeout != newTimeout {
			t.Errorf("Expected timeout %v, got %v", newTimeout, server.timeout)
		}
	})

	t.Run("close server", func(t *testing.T) {
		server, err := NewUdpServer("127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}

		err = server.Close()
		if err != nil {
			t.Errorf("Unexpected error on close: %v", err)
		}

		// Test closing already closed server
		err = server.Close()
		if err != ErrUdpConnectionClosed {
			t.Errorf("Expected ErrUdpConnectionClosed, got %v", err)
		}
	})
}

func TestUdpServerUnsupportedOperations(t *testing.T) {
	server, err := NewUdpServer("127.0.0.1:0")
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

func TestUdpClient(t *testing.T) {
	t.Run("create client", func(t *testing.T) {
		client := NewUdpClient()
		if client == nil {
			t.Error("Expected client but got nil")
			return
		}

		if client.IsConnected() {
			t.Error("New client should not be connected")
		}

		if client.GetAddress() != "" {
			t.Errorf("Expected empty address, got %s", client.GetAddress())
		}
	})

	t.Run("create client with timeout", func(t *testing.T) {
		customTimeout := 5 * time.Second
		client := NewUdpClientWithTimeout(customTimeout)
		if client == nil {
			t.Error("Expected client but got nil")
			return
		}

		if client.UdpCodec.timeout != customTimeout {
			t.Errorf("Expected timeout %v, got %v", customTimeout, client.UdpCodec.timeout)
		}
	})

	t.Run("connect client", func(t *testing.T) {
		// Create a server to connect to
		server, err := NewUdpServer("127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		defer server.Close()

		client := NewUdpClient()

		err = client.Connect(context.Background(), server.RemoteAddr())
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		defer func() {
			if client.UdpCodec != nil {
				client.Close()
			}
		}()

		if !client.IsConnected() {
			t.Error("Client should be connected")
		}

		if client.GetAddress() != server.RemoteAddr() {
			t.Errorf("Expected address %s, got %s", server.RemoteAddr(), client.GetAddress())
		}
	})

	t.Run("client set timeout", func(t *testing.T) {
		client := NewUdpClient()

		// Create a server to connect to
		server, err := NewUdpServer("127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		defer server.Close()

		err = client.Connect(context.Background(), server.RemoteAddr())
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer client.Close()

		newTimeout := 3 * time.Second
		client.SetTimeout(newTimeout)
		if client.UdpCodec.timeout != newTimeout {
			t.Errorf("Expected timeout %v, got %v", newTimeout, client.UdpCodec.timeout)
		}
	})

	t.Run("connect with invalid address", func(t *testing.T) {
		client := NewUdpClient()

		err := client.Connect(context.Background(), "invalid-address")
		if err == nil {
			t.Error("Expected error for invalid address")
			return
		}

		if client.IsConnected() {
			t.Error("Client should not be connected after failed connect")
		}
	})
}

func TestBufferedUdpCodec(t *testing.T) {
	// Create a mock UDP connection
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	baseCodec := NewUdpCodec(conn, nil)

	// Create test request data
	testRequest := &Request{
		JSONRPC: "2.0",
		Method:  "test.method",
		Params:  map[string]any{"arg": "value"},
		ID:      123,
	}

	requestData, err := json.Marshal(testRequest)
	if err != nil {
		t.Fatalf("Failed to marshal test request: %v", err)
	}

	t.Run("read buffered request", func(t *testing.T) {
		bufferedCodec := &BufferedUdpCodec{
			UdpCodec:      baseCodec,
			bufferedData:  requestData,
			hasBufferData: true,
		}

		ctx := context.Background()
		req, err := bufferedCodec.ReadRequest(ctx)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			return
		}

		if req.Method != "test.method" {
			t.Errorf("Expected method test.method, got %s", req.Method)
		}

		if req.ID.(float64) != 123 {
			t.Errorf("Expected ID 123, got %v", req.ID)
		}

		// Verify buffer is consumed
		if bufferedCodec.hasBufferData {
			t.Error("Expected buffer to be consumed")
		}
	})

	t.Run("read buffered response", func(t *testing.T) {
		// Create test response data
		testResponse := &Response{
			JSONRPC: "2.0",
			Result:  "success",
			ID:      123,
		}

		responseData, err := json.Marshal(testResponse)
		if err != nil {
			t.Fatalf("Failed to marshal test response: %v", err)
		}

		bufferedCodec := &BufferedUdpCodec{
			UdpCodec:      baseCodec,
			bufferedData:  responseData,
			hasBufferData: true,
		}

		ctx := context.Background()
		resp, err := bufferedCodec.ReadResponse(ctx)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			return
		}

		if resp.Result != "success" {
			t.Errorf("Expected result success, got %v", resp.Result)
		}

		if resp.ID.(float64) != 123 {
			t.Errorf("Expected ID 123, got %v", resp.ID)
		}

		// Verify buffer is consumed
		if bufferedCodec.hasBufferData {
			t.Error("Expected buffer to be consumed")
		}
	})

	t.Run("invalid buffered data", func(t *testing.T) {
		bufferedCodec := &BufferedUdpCodec{
			UdpCodec:      baseCodec,
			bufferedData:  []byte("invalid json"),
			hasBufferData: true,
		}

		ctx := context.Background()
		_, err := bufferedCodec.ReadRequest(ctx)
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}

		if !strings.Contains(err.Error(), "Parse error") {
			t.Errorf("Expected parse error, got %v", err)
		}
	})
}

func TestUdpCodecTimeout(t *testing.T) {
	// Create server that won't respond
	server, err := NewUdpServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Create client with very short timeout
	client := NewUdpClientWithTimeout(100 * time.Millisecond)
	err = client.Connect(context.Background(), server.RemoteAddr())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Try to read response (should timeout)
	ctx := context.Background()
	_, err = client.ReadResponse(ctx)
	if err == nil {
		t.Error("Expected timeout error")
		return
	}

	// Check if it's a timeout error
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("Expected timeout error, got %v", err)
	}
}

func TestUdpCodecContextCancellation(t *testing.T) {
	// Create server
	server, err := NewUdpServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Create client
	client := NewUdpClient()
	err = client.Connect(context.Background(), server.RemoteAddr())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Try to read response (should be cancelled by context)
	_, err = client.ReadResponse(ctx)
	if err == nil {
		t.Error("Expected context cancellation error")
		return
	}

	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
}

func TestUdpMultipleClients(t *testing.T) {
	// Create server
	server, err := NewUdpServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	serverAddr := server.RemoteAddr()

	// Test multiple clients simultaneously
	numClients := 3
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// Create client
			client := NewUdpClient()
			err := client.Connect(context.Background(), serverAddr)
			if err != nil {
				errors <- err
				return
			}
			defer client.Close()

			// Send request
			request := &Request{
				JSONRPC: "2.0",
				Method:  "test.client",
				Params:  map[string]any{"clientID": clientID},
				ID:      clientID,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err = client.WriteRequest(ctx, request)
			if err != nil {
				errors <- err
				return
			}

			errors <- nil
		}(i)
	}

	// Wait for all clients to complete
	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		if err != nil {
			t.Errorf("Client error: %v", err)
		}
	}
}
