package jsonrpc

import (
	"encoding/json"
	"fmt"
)

// Version represents the JSON-RPC version string
const Version = "2.0"

// Standard error codes as defined in JSON-RPC 2.0 specification
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// Request represents a JSON-RPC 2.0 request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id,omitempty"`
}

// Response represents a JSON-RPC 2.0 response
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      any             `json:"id"`
}

// Error represents a JSON-RPC 2.0 error
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface
func (e *Error) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// NewRequest creates a new JSON-RPC request
func NewRequest(method string, params any, id any) *Request {
	var rawParams json.RawMessage
	if params != nil {
		if raw, ok := params.(json.RawMessage); ok {
			rawParams = raw
		} else {
			// Marshal params to json.RawMessage
			if data, err := json.Marshal(params); err == nil {
				rawParams = data
			}
		}
	}

	return &Request{
		JSONRPC: Version,
		Method:  method,
		Params:  rawParams,
		ID:      id,
	}
}

// NewNotification creates a new JSON-RPC notification (request without ID)
func NewNotification(method string, params any) *Request {
	var rawParams json.RawMessage
	if params != nil {
		if raw, ok := params.(json.RawMessage); ok {
			rawParams = raw
		} else {
			// Marshal params to json.RawMessage
			if data, err := json.Marshal(params); err == nil {
				rawParams = data
			}
		}
	}

	return &Request{
		JSONRPC: Version,
		Method:  method,
		Params:  rawParams,
	}
}

// NewResponse creates a new JSON-RPC response
func NewResponse(result any, id any) *Response {
	var rawResult json.RawMessage
	if result != nil {
		if raw, ok := result.(json.RawMessage); ok {
			rawResult = raw
		} else {
			// Marshal result to json.RawMessage
			if data, err := json.Marshal(result); err == nil {
				rawResult = data
			}
		}
	}

	return &Response{
		JSONRPC: Version,
		Result:  rawResult,
		ID:      id,
	}
}

// NewErrorResponse creates a new JSON-RPC error response
func NewErrorResponse(code int, message string, data any, id any) *Response {
	return &Response{
		JSONRPC: Version,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	}
}

// IsNotification returns true if the request is a notification (has no ID)
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// Validate validates the request according to JSON-RPC 2.0 specification
func (r *Request) Validate() error {
	if r.JSONRPC != Version {
		return &Error{
			Code:    InvalidRequest,
			Message: "Invalid JSON-RPC version",
		}
	}
	if r.Method == "" {
		return &Error{
			Code:    InvalidRequest,
			Message: "Missing method",
		}
	}
	return nil
}

// Validate validates the response according to JSON-RPC 2.0 specification
func (r *Response) Validate() error {
	if r.JSONRPC != Version {
		return &Error{
			Code:    InvalidRequest,
			Message: "Invalid JSON-RPC version",
		}
	}
	if len(r.Result) == 0 && r.Error == nil {
		return &Error{
			Code:    InvalidRequest,
			Message: "Response must have either result or error",
		}
	}
	if len(r.Result) > 0 && r.Error != nil {
		return &Error{
			Code:    InvalidRequest,
			Message: "Response cannot have both result and error",
		}
	}
	return nil
}
