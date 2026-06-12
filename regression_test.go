package jsonrpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// TestRawContextMetadataDoesNotLeakAcrossMessages verifies that metadata
// injected for one message does not leak into the context of subsequent
// messages on the same connection.
func TestRawContextMetadataDoesNotLeakAcrossMessages(t *testing.T) {
	propagator := NewDefaultContextPropagator("trace-id")

	var mu sync.Mutex
	seen := map[string]string{}

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if val, ok := ContextValue(ctx, "trace-id"); ok {
			mu.Lock()
			seen[req.Method] = val
			mu.Unlock()
		}
		return req.CreateResponse(nil)
	})

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	server := NewRawServer(serverConn, handler, WithContextPropagation(propagator))

	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	serverDone := make(chan struct{})
	go func() {
		server.Serve(serverCtx)
		close(serverDone)
	}()

	client := NewRawClient(clientConn, WithContextPropagation(propagator))
	defer client.Close()

	// First call carries metadata.
	ctx := WithContextValue(context.Background(), "trace-id", "trace-1")
	if _, err := client.Call(ctx, WithRequest("withMeta", nil, false)); err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Second call carries no metadata; the handler must not see trace-1.
	if _, err := client.Call(context.Background(), WithRequest("withoutMeta", nil, false)); err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if seen["withMeta"] != "trace-1" {
		t.Errorf("Expected withMeta to see trace-1, got %q", seen["withMeta"])
	}
	if val, ok := seen["withoutMeta"]; ok {
		t.Errorf("Metadata leaked into subsequent message: got trace-id=%q", val)
	}

	cancelServer()
	client.Close()
	<-serverDone
}

// TestCreateResponseMarshalError verifies that an unmarshalable result yields
// an error response rather than a response with neither result nor error.
func TestCreateResponseMarshalError(t *testing.T) {
	req := WithRequest("test", nil, false)
	resp := req.CreateResponse(make(chan int)) // channels cannot be marshaled

	if resp.Error == nil {
		t.Fatal("Expected an error response for unmarshalable result")
	}
	if resp.Error.Code != InternalError {
		t.Errorf("Expected code %d, got %d", InternalError, resp.Error.Code)
	}
	if resp.Result != nil {
		t.Errorf("Expected no result, got %s", resp.Result)
	}
}
