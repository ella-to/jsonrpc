package jsonrpc

import (
	"context"
)

type Codec interface {
	// WriteRequest writes a JSON-RPC request to the underlying transport
	WriteRequest(ctx context.Context, req *Request) error

	// ReadRequest reads a JSON-RPC request from the underlying transport
	ReadRequest(ctx context.Context) (*Request, error)

	// WriteResponse writes a JSON-RPC response to the underlying transport
	WriteResponse(ctx context.Context, resp *Response) error

	// ReadResponse reads a JSON-RPC response from the underlying transport
	ReadResponse(ctx context.Context) (*Response, error)

	// Close closes the underlying transport connection
	Close() error

	// RemoteAddr returns the remote address if available
	RemoteAddr() string
}

// Server defines additional methods needed for server-side codec operations
type ServerCodec interface {
	Codec
	// Accept accepts new connections (for connection-oriented protocols)
	Accept(ctx context.Context) (Codec, error)
}

// Client defines additional methods needed for client-side codec operations
type ClientCodec interface {
	Codec
	// Connect establishes a connection to the server
	Connect(ctx context.Context, address string) error
}
