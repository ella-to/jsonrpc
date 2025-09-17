// Package tcp provides TCP transport implementation for JSON-RPC communication.
package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
)

var (
	// ErrTcpConnectionClosed indicates the connection has been closed
	ErrTcpConnectionClosed = errors.New("tcp: connection closed")
	// ErrTcpInvalidLengthPrefix indicates an invalid length prefix in the message
	ErrTcpInvalidLengthPrefix = errors.New("tcp: invalid length prefix")
	// ErrTcpNotSupported indicates an operation is not supported
	ErrTcpNotSupported = errors.New("tcp: operation not supported")
)

// Codec implements the Codec interface for TCP connections
type TcpCodec struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	mu     sync.RWMutex
}

// New creates a new TCP codec
func NewTcpCodec(conn net.Conn) *TcpCodec {
	return &TcpCodec{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}
}

// WriteRequest writes a JSON-RPC request to the TCP connection
func (c *TcpCodec) WriteRequest(ctx context.Context, req *Request) error {
	return c.writeMessage(req)
}

// ReadRequest reads a JSON-RPC request from the TCP connection
func (c *TcpCodec) ReadRequest(ctx context.Context) (*Request, error) {
	data, err := c.readMessage()
	if err != nil {
		return nil, err
	}

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

// WriteResponse writes a JSON-RPC response to the TCP connection
func (c *TcpCodec) WriteResponse(ctx context.Context, resp *Response) error {
	return c.writeMessage(resp)
}

// ReadResponse reads a JSON-RPC response from the TCP connection
func (c *TcpCodec) ReadResponse(ctx context.Context) (*Response, error) {
	data, err := c.readMessage()
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

// Close closes the TCP connection
func (c *TcpCodec) Close() error {
	if c.conn == nil {
		return ErrTcpConnectionClosed
	}
	return c.conn.Close()
}

// RemoteAddr returns the remote address of the TCP connection
func (c *TcpCodec) RemoteAddr() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.RemoteAddr().String()
}

// writeMessage writes a message with length prefix for framing
func (c *TcpCodec) writeMessage(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return ErrTcpConnectionClosed
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("tcp: failed to marshal message: %w", err)
	}

	// Write message length followed by newline, then the message
	if _, err := fmt.Fprintf(c.writer, "%d\n", len(data)); err != nil {
		return fmt.Errorf("tcp: failed to write length prefix: %w", err)
	}

	if _, err := c.writer.Write(data); err != nil {
		return fmt.Errorf("tcp: failed to write message data: %w", err)
	}

	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("tcp: failed to flush writer: %w", err)
	}

	return nil
}

// readMessage reads a length-prefixed message
func (c *TcpCodec) readMessage() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return nil, ErrTcpConnectionClosed
	}

	// Read the length prefix
	lengthStr, err := c.reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return nil, ErrTcpConnectionClosed
		}
		return nil, fmt.Errorf("tcp: failed to read length prefix: %w", err)
	}

	lengthStr = strings.TrimSpace(lengthStr)
	var length int
	if _, err := fmt.Sscanf(lengthStr, "%d", &length); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTcpInvalidLengthPrefix, lengthStr)
	}

	if length < 0 || length > 1024*1024 { // 1MB limit
		return nil, fmt.Errorf("%w: length %d exceeds limits", ErrTcpInvalidLengthPrefix, length)
	}

	// Read the message data
	data := make([]byte, length)
	if _, err := io.ReadFull(c.reader, data); err != nil {
		return nil, fmt.Errorf("tcp: failed to read message data: %w", err)
	}

	return data, nil
}

// Server implements Server for TCP servers
type TcpServer struct {
	listener net.Listener
	mu       sync.RWMutex
}

// NewServer creates a new TCP server codec
func NewTcpServer(address string) (*TcpServer, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("tcp: failed to listen on %s: %w", address, err)
	}

	return &TcpServer{
		listener: listener,
	}, nil
}

// Accept accepts a new TCP connection and returns a codec for it
func (s *TcpServer) Accept(ctx context.Context) (Codec, error) {
	s.mu.RLock()
	listener := s.listener
	s.mu.RUnlock()

	if listener == nil {
		return nil, ErrTcpConnectionClosed
	}

	// Use a channel to handle context cancellation
	type result struct {
		conn net.Conn
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		conn, err := listener.Accept()
		ch <- result{conn: conn, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return nil, fmt.Errorf("tcp: failed to accept connection: %w", res.err)
		}
		return NewTcpCodec(res.conn), nil
	}
}

// Close closes the TCP listener
func (s *TcpServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener == nil {
		return ErrTcpConnectionClosed
	}

	err := s.listener.Close()
	s.listener = nil
	return err
}

// RemoteAddr returns the listener address
func (s *TcpServer) RemoteAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// WriteRequest, ReadRequest, WriteResponse, ReadResponse are not used in server mode
// but need to be implemented to satisfy the Server interface
func (s *TcpServer) WriteRequest(ctx context.Context, req *Request) error {
	return fmt.Errorf("%w: WriteRequest not supported on server", ErrTcpNotSupported)
}

func (s *TcpServer) ReadRequest(ctx context.Context) (*Request, error) {
	return nil, fmt.Errorf("%w: ReadRequest not supported on server", ErrTcpNotSupported)
}

func (s *TcpServer) WriteResponse(ctx context.Context, resp *Response) error {
	return fmt.Errorf("%w: WriteResponse not supported on server", ErrTcpNotSupported)
}

func (s *TcpServer) ReadResponse(ctx context.Context) (*Response, error) {
	return nil, fmt.Errorf("%w: ReadResponse not supported on server", ErrTcpNotSupported)
}

// Client implements Client for TCP clients
type TcpClient struct {
	*TcpCodec
	address string
	mu      sync.RWMutex
}

// NewClient creates a new TCP client codec
func NewTcpClient() *TcpClient {
	return &TcpClient{}
}

// Connect establishes a TCP connection to the specified address
func (c *TcpClient) Connect(ctx context.Context, address string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("tcp: failed to connect to %s: %w", address, err)
	}

	c.TcpCodec = NewTcpCodec(conn)
	c.address = address
	return nil
}

// IsConnected returns true if the client is connected
func (c *TcpClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.TcpCodec != nil && c.conn != nil
}

// GetAddress returns the connected address
func (c *TcpClient) GetAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.address
}
