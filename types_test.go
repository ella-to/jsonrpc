package jsonrpc

import (
	"bytes"
	"encoding/json"
	"testing"
)

// compareRawMessage compares two json.RawMessage values
func compareRawMessage(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return bytes.Equal(a, b)
}

func TestNewRequest(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		params   any
		id       any
		expected *Request
	}{
		{
			name:   "basic request with string ID",
			method: "test.method",
			params: []any{1, 2, 3},
			id:     "test-id",
			expected: &Request{
				JSONRPC: Version,
				Method:  "test.method",
				Params:  json.RawMessage(`[1,2,3]`),
				ID:      "test-id",
			},
		},
		{
			name:   "request with numeric ID",
			method: "math.add",
			params: map[string]any{"a": 1, "b": 2},
			id:     42,
			expected: &Request{
				JSONRPC: Version,
				Method:  "math.add",
				Params:  json.RawMessage(`{"a":1,"b":2}`),
				ID:      42,
			},
		},
		{
			name:   "request with nil params",
			method: "simple.method",
			params: nil,
			id:     1,
			expected: &Request{
				JSONRPC: Version,
				Method:  "simple.method",
				Params:  nil,
				ID:      1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := NewRequest(tt.method, tt.params, tt.id)
			if req.JSONRPC != tt.expected.JSONRPC {
				t.Errorf("JSONRPC = %v, want %v", req.JSONRPC, tt.expected.JSONRPC)
			}
			if req.Method != tt.expected.Method {
				t.Errorf("Method = %v, want %v", req.Method, tt.expected.Method)
			}
			if req.ID != tt.expected.ID {
				t.Errorf("ID = %v, want %v", req.ID, tt.expected.ID)
			}
			// Compare params as json.RawMessage
			if !compareRawMessage(req.Params, tt.expected.Params) {
				t.Errorf("Params = %s, want %s", string(req.Params), string(tt.expected.Params))
			}
		})
	}
}

func TestNewNotification(t *testing.T) {
	method := "notification.method"
	params := []string{"param1", "param2"}

	notification := NewNotification(method, params)

	if notification.JSONRPC != Version {
		t.Errorf("JSONRPC = %v, want %v", notification.JSONRPC, Version)
	}
	if notification.Method != method {
		t.Errorf("Method = %v, want %v", notification.Method, method)
	}
	if notification.ID != nil {
		t.Errorf("ID = %v, want nil", notification.ID)
	}
	if !notification.IsNotification() {
		t.Error("IsNotification() = false, want true")
	}
}

func TestNewResponse(t *testing.T) {
	result := map[string]any{"answer": 42}
	id := "response-id"

	resp := NewResponse(result, id)

	if resp.JSONRPC != Version {
		t.Errorf("JSONRPC = %v, want %v", resp.JSONRPC, Version)
	}
	if resp.ID != id {
		t.Errorf("ID = %v, want %v", resp.ID, id)
	}
	if resp.Error != nil {
		t.Errorf("Error = %v, want nil", resp.Error)
	}
	// Check that Result is not empty when a result was provided
	if len(resp.Result) == 0 {
		t.Errorf("Result is empty, want non-empty")
	}
}

func TestNewErrorResponse(t *testing.T) {
	code := InvalidParams
	message := "Invalid parameters"
	data := map[string]any{"expected": "array", "received": "object"}
	id := "error-id"

	resp := NewErrorResponse(code, message, data, id)

	if resp.JSONRPC != Version {
		t.Errorf("JSONRPC = %v, want %v", resp.JSONRPC, Version)
	}
	if resp.ID != id {
		t.Errorf("ID = %v, want %v", resp.ID, id)
	}
	if len(resp.Result) != 0 {
		t.Errorf("Result = %s, want empty", string(resp.Result))
	}
	if resp.Error == nil {
		t.Fatal("Error = nil, want non-nil")
	}
	if resp.Error.Code != code {
		t.Errorf("Error.Code = %v, want %v", resp.Error.Code, code)
	}
	if resp.Error.Message != message {
		t.Errorf("Error.Message = %v, want %v", resp.Error.Message, message)
	}
}

func TestRequestIsNotification(t *testing.T) {
	tests := []struct {
		name     string
		request  *Request
		expected bool
	}{
		{
			name: "request with ID is not notification",
			request: &Request{
				JSONRPC: Version,
				Method:  "test",
				ID:      1,
			},
			expected: false,
		},
		{
			name: "request without ID is notification",
			request: &Request{
				JSONRPC: Version,
				Method:  "test",
				ID:      nil,
			},
			expected: true,
		},
		{
			name: "request with zero ID is not notification",
			request: &Request{
				JSONRPC: Version,
				Method:  "test",
				ID:      0,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.request.IsNotification(); got != tt.expected {
				t.Errorf("IsNotification() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRequestValidate(t *testing.T) {
	tests := []struct {
		name      string
		request   *Request
		wantError bool
		errorCode int
	}{
		{
			name: "valid request",
			request: &Request{
				JSONRPC: Version,
				Method:  "test.method",
				ID:      1,
			},
			wantError: false,
		},
		{
			name: "invalid JSONRPC version",
			request: &Request{
				JSONRPC: "1.0",
				Method:  "test.method",
				ID:      1,
			},
			wantError: true,
			errorCode: InvalidRequest,
		},
		{
			name: "missing method",
			request: &Request{
				JSONRPC: Version,
				Method:  "",
				ID:      1,
			},
			wantError: true,
			errorCode: InvalidRequest,
		},
		{
			name: "valid notification",
			request: &Request{
				JSONRPC: Version,
				Method:  "notification.method",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if tt.wantError {
				if err == nil {
					t.Error("Validate() error = nil, want non-nil")
					return
				}
				if rpcErr, ok := err.(*Error); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Error code = %v, want %v", rpcErr.Code, tt.errorCode)
					}
				} else {
					t.Errorf("Error type = %T, want *Error", err)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
			}
		})
	}
}

func TestResponseValidate(t *testing.T) {
	tests := []struct {
		name      string
		response  *Response
		wantError bool
		errorCode int
	}{
		{
			name: "valid response with result",
			response: &Response{
				JSONRPC: Version,
				Result:  json.RawMessage(`"success"`),
				ID:      1,
			},
			wantError: false,
		},
		{
			name: "valid response with error",
			response: &Response{
				JSONRPC: Version,
				Error:   &Error{Code: InternalError, Message: "Internal error"},
				ID:      1,
			},
			wantError: false,
		},
		{
			name: "invalid JSONRPC version",
			response: &Response{
				JSONRPC: "1.0",
				Result:  json.RawMessage(`"success"`),
				ID:      1,
			},
			wantError: true,
			errorCode: InvalidRequest,
		},
		{
			name: "response with both result and error",
			response: &Response{
				JSONRPC: Version,
				Result:  json.RawMessage(`"success"`),
				Error:   &Error{Code: InternalError, Message: "Error"},
				ID:      1,
			},
			wantError: true,
			errorCode: InvalidRequest,
		},
		{
			name: "response with neither result nor error",
			response: &Response{
				JSONRPC: Version,
				ID:      1,
			},
			wantError: true,
			errorCode: InvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.response.Validate()
			if tt.wantError {
				if err == nil {
					t.Error("Validate() error = nil, want non-nil")
					return
				}
				if rpcErr, ok := err.(*Error); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Error code = %v, want %v", rpcErr.Code, tt.errorCode)
					}
				} else {
					t.Errorf("Error type = %T, want *Error", err)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
			}
		})
	}
}

func TestErrorError(t *testing.T) {
	err := &Error{
		Code:    MethodNotFound,
		Message: "Method not found",
		Data:    "additional info",
	}

	expected := "JSON-RPC error -32601: Method not found"
	if got := err.Error(); got != expected {
		t.Errorf("Error() = %v, want %v", got, expected)
	}
}

func TestRequestJSONMarshaling(t *testing.T) {
	tests := []struct {
		name    string
		request *Request
	}{
		{
			name: "request with all fields",
			request: &Request{
				JSONRPC: Version,
				Method:  "test.method",
				Params:  json.RawMessage(`[1,"test",true]`),
				ID:      "test-id",
			},
		},
		{
			name: "notification without ID",
			request: &Request{
				JSONRPC: Version,
				Method:  "notification.method",
				Params:  json.RawMessage(`{"key":"value"}`),
			},
		},
		{
			name: "request without params",
			request: &Request{
				JSONRPC: Version,
				Method:  "simple.method",
				ID:      42,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Marshal error = %v", err)
			}

			// Unmarshal back
			var unmarshaled Request
			err = json.Unmarshal(data, &unmarshaled)
			if err != nil {
				t.Fatalf("Unmarshal error = %v", err)
			}

			// Compare fields
			if unmarshaled.JSONRPC != tt.request.JSONRPC {
				t.Errorf("JSONRPC = %v, want %v", unmarshaled.JSONRPC, tt.request.JSONRPC)
			}
			if unmarshaled.Method != tt.request.Method {
				t.Errorf("Method = %v, want %v", unmarshaled.Method, tt.request.Method)
			}

			// Handle ID comparison carefully for numeric types
			// JSON unmarshaling converts numbers to float64
			if tt.request.ID != nil {
				switch originalID := tt.request.ID.(type) {
				case int:
					if unmarshaledFloat, ok := unmarshaled.ID.(float64); ok {
						if int(unmarshaledFloat) != originalID {
							t.Errorf("ID = %v, want %v", unmarshaled.ID, tt.request.ID)
						}
					} else {
						t.Errorf("ID type = %T, want float64 (from JSON)", unmarshaled.ID)
					}
				default:
					if unmarshaled.ID != tt.request.ID {
						t.Errorf("ID = %v, want %v", unmarshaled.ID, tt.request.ID)
					}
				}
			} else {
				if unmarshaled.ID != nil {
					t.Errorf("ID = %v, want nil", unmarshaled.ID)
				}
			}
		})
	}
}

func TestResponseJSONMarshaling(t *testing.T) {
	tests := []struct {
		name     string
		response *Response
	}{
		{
			name: "response with result",
			response: &Response{
				JSONRPC: Version,
				Result:  json.RawMessage(`{"answer":42}`),
				ID:      "test-id",
			},
		},
		{
			name: "response with error",
			response: &Response{
				JSONRPC: Version,
				Error: &Error{
					Code:    InvalidParams,
					Message: "Invalid parameters",
					Data:    "additional data",
				},
				ID: 123,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("Marshal error = %v", err)
			}

			// Unmarshal back
			var unmarshaled Response
			err = json.Unmarshal(data, &unmarshaled)
			if err != nil {
				t.Fatalf("Unmarshal error = %v", err)
			}

			// Compare fields
			if unmarshaled.JSONRPC != tt.response.JSONRPC {
				t.Errorf("JSONRPC = %v, want %v", unmarshaled.JSONRPC, tt.response.JSONRPC)
			}

			// Handle ID comparison carefully for numeric types
			// JSON unmarshaling converts numbers to float64
			if tt.response.ID != nil {
				switch originalID := tt.response.ID.(type) {
				case int:
					if unmarshaledFloat, ok := unmarshaled.ID.(float64); ok {
						if int(unmarshaledFloat) != originalID {
							t.Errorf("ID = %v, want %v", unmarshaled.ID, tt.response.ID)
						}
					} else {
						t.Errorf("ID type = %T, want float64 (from JSON)", unmarshaled.ID)
					}
				default:
					if unmarshaled.ID != tt.response.ID {
						t.Errorf("ID = %v, want %v", unmarshaled.ID, tt.response.ID)
					}
				}
			} else {
				if unmarshaled.ID != nil {
					t.Errorf("ID = %v, want nil", unmarshaled.ID)
				}
			}
		})
	}
}

func TestConstants(t *testing.T) {
	// Test that constants have expected values according to JSON-RPC 2.0 spec
	if Version != "2.0" {
		t.Errorf("Version = %v, want 2.0", Version)
	}

	expectedCodes := map[string]int{
		"ParseError":     -32700,
		"InvalidRequest": -32600,
		"MethodNotFound": -32601,
		"InvalidParams":  -32602,
		"InternalError":  -32603,
	}

	actualCodes := map[string]int{
		"ParseError":     ParseError,
		"InvalidRequest": InvalidRequest,
		"MethodNotFound": MethodNotFound,
		"InvalidParams":  InvalidParams,
		"InternalError":  InternalError,
	}

	for name, expected := range expectedCodes {
		if actual := actualCodes[name]; actual != expected {
			t.Errorf("%s = %v, want %v", name, actual, expected)
		}
	}
}

func TestErrorJSONMarshaling(t *testing.T) {
	err := &Error{
		Code:    MethodNotFound,
		Message: "Method not found",
		Data:    map[string]any{"method": "unknown.method"},
	}

	// Marshal to JSON
	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("Marshal error = %v", marshalErr)
	}

	// Unmarshal back
	var unmarshaled Error
	unmarshalErr := json.Unmarshal(data, &unmarshaled)
	if unmarshalErr != nil {
		t.Fatalf("Unmarshal error = %v", unmarshalErr)
	}

	// Compare fields
	if unmarshaled.Code != err.Code {
		t.Errorf("Code = %v, want %v", unmarshaled.Code, err.Code)
	}
	if unmarshaled.Message != err.Message {
		t.Errorf("Message = %v, want %v", unmarshaled.Message, err.Message)
	}
}
