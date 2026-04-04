package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/rs/xid"
)

// LibVersion represents the current version of the jsonrpc library.
const LibVersion = "0.0.3"

// Version represents the JSON-RPC protocol version supported by this package.
const Version = "2.0"

// Standard error codes as defined in the JSON-RPC 2.0 specification.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// Request represents a JSON-RPC 2.0 request message.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id,omitempty"`
}

func (r *Request) CreateResponse(result any) *Response {
	resp := &Response{
		JSONRPC: Version,
		ID:      r.ID,
	}

	if result == nil {
		resp.Result = json.RawMessage("null")
		return resp
	}

	raw, _ := json.Marshal(result)
	resp.Result = raw
	return resp
}

func (r *Request) CreateErrorResponse(err error) *Response {
	resp := &Response{
		JSONRPC: Version,
		ID:      r.ID,
	}

	resp.Error = toRPCError(err)
	return resp
}

// toRPCError converts an error to a *Error, handling joined errors by
// chaining them hierarchically (each subsequent error becomes the Cause).
func toRPCError(err error) *Error {
	if err == nil {
		return nil
	}

	// Check if it's a joined error (implements Unwrap() []error)
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs := joined.Unwrap()
		if len(errs) > 0 {
			return chainErrors(errs)
		}
	}

	// Check if it's already a *Error
	var rpcErr *Error
	if errors.As(err, &rpcErr) {
		return rpcErr
	}

	// Wrap as internal error
	return &Error{
		Code:    InternalError,
		Message: err.Error(),
	}
}

// chainErrors converts a slice of errors into a hierarchical *Error chain.
func chainErrors(errs []error) *Error {
	if len(errs) == 0 {
		return nil
	}

	// Convert first error to *Error (this will be the root)
	var root *Error
	if rpcErr, ok := errs[0].(*Error); ok {
		// Clone to avoid mutating the original
		clone := *rpcErr
		root = &clone
	} else {
		root = &Error{
			Code:    InternalError,
			Message: errs[0].Error(),
		}
	}

	// Chain the rest as causes
	current := root
	for i := 1; i < len(errs); i++ {
		var next *Error
		if rpcErr, ok := errs[i].(*Error); ok {
			clone := *rpcErr
			next = &clone
		} else {
			next = &Error{
				Code:    InternalError,
				Message: errs[i].Error(),
			}
		}
		current.Cause = next
		current = next
	}

	return root
}

// Response represents a JSON-RPC 2.0 response message.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      any             `json:"id"`
}

// Error represents a JSON-RPC 2.0 error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"cause,omitempty"`
}

// Error implements the error interface for JSON-RPC error objects.

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%d: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%d: %s", e.Code, e.Message)
}

func (e Error) Is(target error) bool {
	if target == nil {
		return false
	}

	// Check if target is a joined error (implements Unwrap() []error)
	if joined, ok := target.(interface{ Unwrap() []error }); ok {
		return slices.ContainsFunc(joined.Unwrap(), e.Is)
	}

	if rpcErr, ok := target.(*Error); ok {
		return rpcErr.Code == e.Code
	}
	return errors.Is(e.Cause, target)
}

func (e Error) Unwrap() error {
	return e.Cause
}

// WithCause returns a new Error with the specified cause.
// if cause is nil, it returns nil.
func (e Error) WithCause(cause error) error {
	if cause == nil {
		return nil
	}
	err := e
	err.Cause = cause
	return &err
}

func (e Error) WithMsg(format string, args ...any) error {
	err := e
	err.Message = fmt.Sprintf(format, args...)
	return &err
}

func (e *Error) MarshalJSON() ([]byte, error) {
	payload := struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Cause   string `json:"cause,omitempty"`
	}{}

	payload.Code = e.Code
	payload.Message = e.Message
	if e.Cause != nil {
		payload.Cause = e.Cause.Error()
	}

	return json.Marshal(payload)
}

func (e *Error) UnmarshalJSON(data []byte) error {
	wrapper := struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Cause   string `json:"cause,omitempty"`
	}{}

	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}

	e.Message = wrapper.Message
	e.Code = wrapper.Code
	if wrapper.Cause != "" {
		e.Cause = errors.New(wrapper.Cause)
	} else {
		e.Cause = nil
	}

	return nil
}

func NewError(code int, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Cause:   nil,
	}
}

// Handler serves JSON-RPC requests.
type Handler interface {
	Handle(ctx context.Context, req *Request) *Response
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(ctx context.Context, req *Request) *Response

// Handle dispatches the request to f.
func (f HandlerFunc) Handle(ctx context.Context, req *Request) *Response {
	return f(ctx, req)
}

func WithRequest(method string, params any, isNotify bool) *Request {
	req := &Request{
		JSONRPC: Version,
		Method:  method,
	}

	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			panic(err)
		}
		req.Params = raw
	}

	if !isNotify {
		req.ID = xid.New().String()
	}

	return req
}

// Caller issues JSON-RPC requests and returns their responses.
type Caller interface {
	Call(ctx context.Context, requests ...*Request) ([]*Response, error)
}

func NormalizeID(id any) (string, bool) {
	switch v := id.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case float64:
		if math.Trunc(v) != v {
			return "", false
		}
		return strconv.FormatInt(int64(v), 10), true
	case int:
		return strconv.Itoa(v), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	default:
		return "", false
	}
}
