// Package udp provides UDP transport implementation for JSON-RPC communication.
//
// Example Usage:
//
// Server:
//
//	server, err := NewUdpServer("127.0.0.1:8080")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer server.Close()
//
//	for {
//	    codec, err := server.Accept(context.Background())
//	    if err != nil {
//	        log.Printf("Accept error: %v", err)
//	        continue
//	    }
//
//	    go func() {
//	        defer codec.Close()
//	        req, err := codec.ReadRequest(context.Background())
//	        if err != nil {
//	            log.Printf("Read error: %v", err)
//	            return
//	        }
//
//	        // Process request and create response
//	        var params map[string]any
//	        if err := json.Unmarshal(req.Params, &params); err != nil {
//	            // Handle error
//	            return
//	        }
//	        resp := &Response{
//	            JSONRPC: "2.0",
//	            Result:  json.RawMessage(`"Hello ` + params["name"].(string) + `"`),
//	            ID:      req.ID,
//	        }
//
//	        if err := codec.WriteResponse(context.Background(), resp); err != nil {
//	            log.Printf("Write error: %v", err)
//	        }
//	    }()
//	}
//
// Client:
//
//	client := NewUdpClient()
//	if err := client.Connect(context.Background(), "127.0.0.1:8080"); err != nil {
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
package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

var (
	// ErrUdpConnectionClosed indicates the connection has been closed
	ErrUdpConnectionClosed = errors.New("udp: connection closed")
	// ErrUdpNotSupported indicates an operation is not supported
	ErrUdpNotSupported = errors.New("udp: operation not supported")
	// ErrUdpMessageTooLarge indicates the message exceeds UDP packet size limits
	ErrUdpMessageTooLarge = errors.New("udp: message too large for UDP packet")
	// ErrUdpTimeout indicates a timeout occurred
	ErrUdpTimeout = errors.New("udp: operation timed out")
)

const (
	// MaxUDPPacketSize is the maximum size for UDP packets (safe size for most networks)
	MaxUDPPacketSize = 1400
	// DefaultTimeout is the default timeout for UDP operations
	DefaultTimeout = 30 * time.Second
)

// UdpCodec implements the Codec interface for UDP connections
type UdpCodec struct {
	conn       *net.UDPConn
	remoteAddr *net.UDPAddr
	localAddr  *net.UDPAddr
	timeout    time.Duration
	mu         sync.RWMutex
	closed     bool
}

// NewUdpCodec creates a new UDP codec with a UDP connection
func NewUdpCodec(conn *net.UDPConn, remoteAddr *net.UDPAddr) *UdpCodec {
	var localAddr *net.UDPAddr
	if conn.LocalAddr() != nil {
		localAddr = conn.LocalAddr().(*net.UDPAddr)
	}

	return &UdpCodec{
		conn:       conn,
		remoteAddr: remoteAddr,
		localAddr:  localAddr,
		timeout:    DefaultTimeout,
	}
}

// NewUdpCodecWithTimeout creates a new UDP codec with a custom timeout
func NewUdpCodecWithTimeout(conn *net.UDPConn, remoteAddr *net.UDPAddr, timeout time.Duration) *UdpCodec {
	codec := NewUdpCodec(conn, remoteAddr)
	codec.timeout = timeout
	return codec
}

// WriteRequest writes a JSON-RPC request to the UDP connection
func (c *UdpCodec) WriteRequest(ctx context.Context, req *Request) error {
	return c.writeMessage(ctx, req)
}

// ReadRequest reads a JSON-RPC request from the UDP connection
func (c *UdpCodec) ReadRequest(ctx context.Context) (*Request, error) {
	data, remoteAddr, err := c.readMessage(ctx)
	if err != nil {
		return nil, err
	}

	// Update remote address if we didn't have one
	c.mu.Lock()
	if c.remoteAddr == nil {
		c.remoteAddr = remoteAddr
	}
	c.mu.Unlock()

	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
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

// WriteResponse writes a JSON-RPC response to the UDP connection
func (c *UdpCodec) WriteResponse(ctx context.Context, resp *Response) error {
	return c.writeMessage(ctx, resp)
}

// ReadResponse reads a JSON-RPC response from the UDP connection
func (c *UdpCodec) ReadResponse(ctx context.Context) (*Response, error) {
	data, _, err := c.readMessage(ctx)
	if err != nil {
		return nil, err
	}

	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
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

// Close closes the UDP connection
func (c *UdpCodec) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.conn == nil {
		return ErrUdpConnectionClosed
	}

	c.closed = true
	return c.conn.Close()
}

// RemoteAddr returns the remote address of the UDP connection
func (c *UdpCodec) RemoteAddr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.remoteAddr == nil {
		return ""
	}
	return c.remoteAddr.String()
}

// LocalAddr returns the local address of the UDP connection
func (c *UdpCodec) LocalAddr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.localAddr == nil {
		return ""
	}
	return c.localAddr.String()
}

// SetTimeout sets the timeout for UDP operations
func (c *UdpCodec) SetTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = timeout
}

// writeMessage writes a message to the UDP connection
func (c *UdpCodec) writeMessage(ctx context.Context, v any) error {
	c.mu.RLock()
	conn := c.conn
	remoteAddr := c.remoteAddr
	timeout := c.timeout
	closed := c.closed
	c.mu.RUnlock()

	if closed || conn == nil {
		return ErrUdpConnectionClosed
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("udp: failed to marshal message: %w", err)
	}

	if len(data) > MaxUDPPacketSize {
		return fmt.Errorf("%w: message size %d exceeds limit %d", ErrUdpMessageTooLarge, len(data), MaxUDPPacketSize)
	}

	// Set deadline for write operation
	deadline := time.Now().Add(timeout)
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("udp: failed to set write deadline: %w", err)
	}

	// Create a channel to handle the write operation
	done := make(chan error, 1)
	go func() {
		var err error
		if remoteAddr != nil {
			// Use WriteToUDP for unconnected sockets
			_, err = conn.WriteToUDP(data, remoteAddr)
		} else {
			// Use Write for connected sockets
			_, err = conn.Write(data)
		}
		done <- err
	}()

	// Wait for completion or context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("udp: failed to write message: %w", err)
		}
		return nil
	}
}

// readMessage reads a message from the UDP connection
func (c *UdpCodec) readMessage(ctx context.Context) ([]byte, *net.UDPAddr, error) {
	c.mu.RLock()
	conn := c.conn
	timeout := c.timeout
	closed := c.closed
	c.mu.RUnlock()

	if closed || conn == nil {
		return nil, nil, ErrUdpConnectionClosed
	}

	// Set deadline for read operation
	deadline := time.Now().Add(timeout)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, nil, fmt.Errorf("udp: failed to set read deadline: %w", err)
	}

	// Create a channel to handle the read operation
	type readResult struct {
		data []byte
		addr *net.UDPAddr
		err  error
	}

	done := make(chan readResult, 1)
	go func() {
		buffer := make([]byte, MaxUDPPacketSize)

		// Try ReadFromUDP first (for server-side unconnected sockets)
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			// If that fails and we don't have a remote address, try Read (for connected sockets)
			if c.remoteAddr == nil {
				n, err = conn.Read(buffer)
				if err != nil {
					done <- readResult{err: err}
					return
				}
				// For connected sockets, remote address comes from the connection
				if conn.RemoteAddr() != nil {
					addr = conn.RemoteAddr().(*net.UDPAddr)
				}
			} else {
				done <- readResult{err: err}
				return
			}
		}
		done <- readResult{data: buffer[:n], addr: addr}
	}()

	// Wait for completion or context cancellation
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case result := <-done:
		if result.err != nil {
			return nil, nil, fmt.Errorf("udp: failed to read message: %w", result.err)
		}
		return result.data, result.addr, nil
	}
}

// UdpServer implements ServerCodec for UDP servers
type UdpServer struct {
	conn    *net.UDPConn
	address string
	timeout time.Duration
	mu      sync.RWMutex
	closed  bool
}

// NewUdpServer creates a new UDP server codec
func NewUdpServer(address string) (*UdpServer, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("udp: failed to resolve address %s: %w", address, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("udp: failed to listen on %s: %w", address, err)
	}

	return &UdpServer{
		conn:    conn,
		address: conn.LocalAddr().String(),
		timeout: DefaultTimeout,
	}, nil
}

// NewUdpServerWithTimeout creates a new UDP server codec with custom timeout
func NewUdpServerWithTimeout(address string, timeout time.Duration) (*UdpServer, error) {
	server, err := NewUdpServer(address)
	if err != nil {
		return nil, err
	}
	server.timeout = timeout
	return server, nil
}

// Accept waits for and returns a codec for handling a UDP message
// Since UDP is connectionless, this creates a new codec for each message
func (s *UdpServer) Accept(ctx context.Context) (Codec, error) {
	s.mu.RLock()
	conn := s.conn
	timeout := s.timeout
	closed := s.closed
	s.mu.RUnlock()

	if closed || conn == nil {
		return nil, ErrUdpConnectionClosed
	}

	// Set deadline for read operation
	deadline := time.Now().Add(timeout)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, fmt.Errorf("udp: failed to set read deadline: %w", err)
	}

	// Create a channel to handle the read operation
	type acceptResult struct {
		codec Codec
		err   error
	}

	done := make(chan acceptResult, 1)
	go func() {
		buffer := make([]byte, MaxUDPPacketSize)
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			done <- acceptResult{err: fmt.Errorf("udp: failed to read initial message: %w", err)}
			return
		}

		// Create a new codec for this client
		codec := NewUdpCodecWithTimeout(conn, remoteAddr, timeout)

		// Store the first message in a buffer-like codec
		bufferedCodec := &BufferedUdpCodec{
			UdpCodec:      codec,
			bufferedData:  buffer[:n],
			hasBufferData: true,
		}

		done <- acceptResult{codec: bufferedCodec}
	}()

	// Wait for completion or context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-done:
		return result.codec, result.err
	}
}

// Close closes the UDP server
func (s *UdpServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.conn == nil {
		return ErrUdpConnectionClosed
	}

	s.closed = true
	return s.conn.Close()
}

// RemoteAddr returns the server address (for UDP server, this is the local listening address)
func (s *UdpServer) RemoteAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.address
}

// SetTimeout sets the timeout for server operations
func (s *UdpServer) SetTimeout(timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timeout = timeout
}

// WriteRequest, ReadRequest, WriteResponse, ReadResponse are not used in server mode
// but need to be implemented to satisfy the ServerCodec interface
func (s *UdpServer) WriteRequest(ctx context.Context, req *Request) error {
	return fmt.Errorf("%w: WriteRequest not supported on server", ErrUdpNotSupported)
}

func (s *UdpServer) ReadRequest(ctx context.Context) (*Request, error) {
	return nil, fmt.Errorf("%w: ReadRequest not supported on server", ErrUdpNotSupported)
}

func (s *UdpServer) WriteResponse(ctx context.Context, resp *Response) error {
	return fmt.Errorf("%w: WriteResponse not supported on server", ErrUdpNotSupported)
}

func (s *UdpServer) ReadResponse(ctx context.Context) (*Response, error) {
	return nil, fmt.Errorf("%w: ReadResponse not supported on server", ErrUdpNotSupported)
}

// BufferedUdpCodec wraps UdpCodec to handle the first message received during Accept
type BufferedUdpCodec struct {
	*UdpCodec
	bufferedData  []byte
	hasBufferData bool
	mu            sync.Mutex
}

// ReadRequest reads the buffered request first, then normal requests
func (c *BufferedUdpCodec) ReadRequest(ctx context.Context) (*Request, error) {
	c.mu.Lock()
	if c.hasBufferData {
		data := c.bufferedData
		c.hasBufferData = false
		c.mu.Unlock()

		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
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
	c.mu.Unlock()

	// No buffered data, read normally
	return c.UdpCodec.ReadRequest(ctx)
}

// ReadResponse reads the buffered response first, then normal responses
func (c *BufferedUdpCodec) ReadResponse(ctx context.Context) (*Response, error) {
	c.mu.Lock()
	if c.hasBufferData {
		data := c.bufferedData
		c.hasBufferData = false
		c.mu.Unlock()

		var resp Response
		if err := json.Unmarshal(data, &resp); err != nil {
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
	c.mu.Unlock()

	// No buffered data, read normally
	return c.UdpCodec.ReadResponse(ctx)
}

// UdpClient implements ClientCodec for UDP clients
type UdpClient struct {
	*UdpCodec
	address string
	mu      sync.RWMutex
}

// NewUdpClient creates a new UDP client codec
func NewUdpClient() *UdpClient {
	return &UdpClient{}
}

// NewUdpClientWithTimeout creates a new UDP client codec with custom timeout
func NewUdpClientWithTimeout(timeout time.Duration) *UdpClient {
	return &UdpClient{
		UdpCodec: &UdpCodec{timeout: timeout},
	}
}

// Connect establishes a UDP connection to the specified address
func (c *UdpClient) Connect(ctx context.Context, address string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	remoteAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("udp: failed to resolve address %s: %w", address, err)
	}

	conn, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		return fmt.Errorf("udp: failed to connect to %s: %w", address, err)
	}

	timeout := DefaultTimeout
	if c.UdpCodec != nil {
		timeout = c.UdpCodec.timeout
	}

	// For connected UDP sockets, we don't need to specify remoteAddr for writes
	c.UdpCodec = NewUdpCodecWithTimeout(conn, nil, timeout)
	c.address = address
	return nil
}

// IsConnected returns true if the client is connected
func (c *UdpClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.UdpCodec != nil && c.conn != nil && !c.closed
}

// GetAddress returns the connected address
func (c *UdpClient) GetAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.address
}

// SetTimeout sets the timeout for client operations
func (c *UdpClient) SetTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.UdpCodec != nil {
		c.UdpCodec.SetTimeout(timeout)
	}
}
