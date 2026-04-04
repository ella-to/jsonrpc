package jsonrpc

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestRawContextPropagation(t *testing.T) {
	// Create a propagator that propagates specific keys
	propagator := NewDefaultContextPropagator("request-id", "session-id")

	// Create a handler that checks if context values are present
	var receivedValues sync.Map
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if val, ok := ContextValue(ctx, "request-id"); ok {
			receivedValues.Store("request-id", val)
		}
		if val, ok := ContextValue(ctx, "session-id"); ok {
			receivedValues.Store("session-id", val)
		}

		return req.CreateResponse(map[string]string{"status": "ok"})
	})

	// Create a pipe for communication
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Create server with context propagation
	server := NewRawServer(serverConn, handler, WithContextPropagation(propagator))

	// Start server in background
	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	serverDone := make(chan struct{})
	go func() {
		_ = server.Serve(serverCtx)
		close(serverDone)
	}()

	// Create client with context propagation
	client := NewRawClient(clientConn, WithContextPropagation(propagator))
	defer client.Close()

	// Create a context with values to propagate
	ctx := context.Background()
	ctx = WithContextValue(ctx, "request-id", "raw-req-123")
	ctx = WithContextValue(ctx, "session-id", "session-456")

	// Make a request
	req := WithRequest("test.method", map[string]string{"param": "value"}, false)
	responses, err := client.Call(ctx, req)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(responses))
	}

	if responses[0].Error != nil {
		t.Fatalf("Unexpected error: %v", responses[0].Error)
	}

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	// Verify the values were propagated
	if val, ok := receivedValues.Load("request-id"); !ok || val != "raw-req-123" {
		t.Errorf("Expected request-id=raw-req-123, got %v", val)
	}
	if val, ok := receivedValues.Load("session-id"); !ok || val != "session-456" {
		t.Errorf("Expected session-id=session-456, got %v", val)
	}

	// Cleanup
	cancelServer()
	client.Close()
	<-serverDone
}

func TestRawContextPropagationWithBatch(t *testing.T) {
	propagator := NewDefaultContextPropagator("batch-id")

	var batchID sync.Map
	callCount := 0
	var mu sync.Mutex

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		mu.Lock()
		callCount++
		mu.Unlock()

		if val, ok := ContextValue(ctx, "batch-id"); ok {
			batchID.Store(req.Method, val)
		}
		return req.CreateResponse(map[string]string{"method": req.Method})
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

	ctx := context.Background()
	ctx = WithContextValue(ctx, "batch-id", "batch-777")

	// Send batch request
	req1 := WithRequest("method1", nil, false)
	req2 := WithRequest("method2", nil, false)
	req3 := WithRequest("method3", nil, false)

	responses, err := client.Call(ctx, req1, req2, req3)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(responses) != 3 {
		t.Fatalf("Expected 3 responses, got %d", len(responses))
	}

	// Give server time to process all requests
	time.Sleep(200 * time.Millisecond)

	// All handlers should have received the batch ID
	for _, method := range []string{"method1", "method2", "method3"} {
		if val, ok := batchID.Load(method); !ok || val != "batch-777" {
			t.Errorf("Expected batch-id=batch-777 for %s, got %v", method, val)
		}
	}

	mu.Lock()
	if callCount != 3 {
		t.Errorf("Expected 3 handler calls, got %d", callCount)
	}
	mu.Unlock()

	cancelServer()
	client.Close()
	<-serverDone
}

func TestRawContextPropagationWithoutPropagator(t *testing.T) {
	// Handler that checks for context values
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		// Should NOT have the propagated value when propagator is not set
		if val, ok := ContextValue(ctx, "should-not-exist"); ok {
			t.Errorf("Unexpected value found: %s", val)
		}
		return req.CreateResponse(map[string]string{"status": "ok"})
	})

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Create server WITHOUT context propagation
	server := NewRawServer(serverConn, handler)

	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	serverDone := make(chan struct{})
	go func() {
		server.Serve(serverCtx)
		close(serverDone)
	}()

	// Create client WITHOUT context propagation
	client := NewRawClient(clientConn)
	defer client.Close()

	ctx := context.Background()
	ctx = WithContextValue(ctx, "should-not-exist", "test-value")

	req := WithRequest("test.method", nil, false)
	responses, err := client.Call(ctx, req)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(responses))
	}

	cancelServer()
	client.Close()
	<-serverDone
}

func TestRawContextPropagationMultipleRequests(t *testing.T) {
	propagator := NewDefaultContextPropagator("trace-id")

	var receivedTraces sync.Map

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if val, ok := ContextValue(ctx, "trace-id"); ok {
			receivedTraces.Store(req.Method, val)
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

	// Send multiple requests with different trace IDs
	for i := 1; i <= 3; i++ {
		ctx := context.Background()
		traceID := "trace-" + string(rune('0'+i))
		ctx = WithContextValue(ctx, "trace-id", traceID)

		method := "method" + string(rune('0'+i))
		req := WithRequest(method, nil, false)

		responses, err := client.Call(ctx, req)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}

		if len(responses) != 1 {
			t.Fatalf("Expected 1 response for call %d, got %d", i, len(responses))
		}
	}

	// Give server time to process
	time.Sleep(200 * time.Millisecond)

	// Verify each request had its own trace ID
	for i := 1; i <= 3; i++ {
		method := "method" + string(rune('0'+i))
		expectedTrace := "trace-" + string(rune('0'+i))

		if val, ok := receivedTraces.Load(method); !ok || val != expectedTrace {
			t.Errorf("Expected %s=%s, got %v", method, expectedTrace, val)
		}
	}

	cancelServer()
	client.Close()
	<-serverDone
}

func TestRawContextPropagationWithNotification(t *testing.T) {
	propagator := NewDefaultContextPropagator("notification-id")

	var receivedID string
	var mu sync.Mutex

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if val, ok := ContextValue(ctx, "notification-id"); ok {
			mu.Lock()
			receivedID = val
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

	ctx := context.Background()
	ctx = WithContextValue(ctx, "notification-id", "notif-999")

	// Send a notification (no response expected)
	notif := WithRequest("notify.method", nil, true) // true = isNotify
	responses, err := client.Call(ctx, notif)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(responses) != 1 || responses[0] != nil {
		t.Fatalf("Expected nil response for notification")
	}

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if receivedID != "notif-999" {
		t.Errorf("Expected notification-id=notif-999, got %s", receivedID)
	}
	mu.Unlock()

	cancelServer()
	client.Close()
	<-serverDone
}

func TestRawContextPropagationEdgeCases(t *testing.T) {
	t.Run("EmptyMetadata", func(t *testing.T) {
		propagator := NewDefaultContextPropagator("non-existent-key")

		handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
			return req.CreateResponse(nil)
		})

		clientConn, serverConn := net.Pipe()
		defer clientConn.Close()
		defer serverConn.Close()

		server := NewRawServer(serverConn, handler, WithContextPropagation(propagator))

		serverCtx, cancelServer := context.WithCancel(context.Background())
		defer cancelServer()

		go server.Serve(serverCtx)

		client := NewRawClient(clientConn, WithContextPropagation(propagator))
		defer client.Close()

		// Context without the expected key
		ctx := context.Background()
		req := WithRequest("test.method", nil, false)

		responses, err := client.Call(ctx, req)
		if err != nil {
			t.Fatalf("Call failed: %v", err)
		}

		if len(responses) != 1 {
			t.Fatalf("Expected 1 response, got %d", len(responses))
		}

		cancelServer()
	})

	t.Run("NilContext", func(t *testing.T) {
		propagator := NewDefaultContextPropagator("test-key")

		metadata := propagator.Extract(nil)
		if metadata != nil {
			t.Error("Expected nil metadata from nil context")
		}
	})
}

func TestMetadataSerializationDeserialization(t *testing.T) {
	metadata := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	// Test serialization
	data, err := serializeMetadata(metadata)
	if err != nil {
		t.Fatalf("Failed to serialize metadata: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("Expected non-empty serialized data")
	}

	// Test deserialization
	deserialized, err := deserializeMetadata(data)
	if err != nil {
		t.Fatalf("Failed to deserialize metadata: %v", err)
	}

	if len(deserialized) != len(metadata) {
		t.Errorf("Expected %d entries, got %d", len(metadata), len(deserialized))
	}

	for key, expectedVal := range metadata {
		if actualVal, ok := deserialized[key]; !ok || actualVal != expectedVal {
			t.Errorf("Expected %s=%s, got %s", key, expectedVal, actualVal)
		}
	}
}

func TestMetadataSerializationEmpty(t *testing.T) {
	// Test with nil metadata
	data, err := serializeMetadata(nil)
	if err != nil {
		t.Fatalf("Failed to serialize nil metadata: %v", err)
	}
	if data != nil {
		t.Error("Expected nil data from nil metadata")
	}

	// Test with empty metadata
	data, err = serializeMetadata(map[string]string{})
	if err != nil {
		t.Fatalf("Failed to serialize empty metadata: %v", err)
	}
	if data != nil {
		t.Error("Expected nil data from empty metadata")
	}

	// Test deserializing nil data
	metadata, err := deserializeMetadata(nil)
	if err != nil {
		t.Fatalf("Failed to deserialize nil data: %v", err)
	}
	if metadata != nil {
		t.Error("Expected nil metadata from nil data")
	}

	// Test deserializing empty data
	metadata, err = deserializeMetadata([]byte{})
	if err != nil {
		t.Fatalf("Failed to deserialize empty data: %v", err)
	}
	if metadata != nil {
		t.Error("Expected nil metadata from empty data")
	}
}

// TestRawContextPropagationConcurrent tests concurrent requests with context propagation
func TestRawContextPropagationConcurrent(t *testing.T) {
	propagator := NewDefaultContextPropagator("worker-id")

	var receivedIDs sync.Map
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if val, ok := ContextValue(ctx, "worker-id"); ok {
			receivedIDs.Store(req.Method, val)
		}
		// Simulate some work
		time.Sleep(10 * time.Millisecond)
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

	// Send multiple concurrent requests
	const numWorkers = 5
	var wg sync.WaitGroup
	errors := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			ctx := context.Background()
			ctx = WithContextValue(ctx, "worker-id", "worker-"+string(rune('0'+workerID)))

			method := "worker.method" + string(rune('0'+workerID))
			req := WithRequest(method, nil, false)

			_, err := client.Call(ctx, req)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		if err != nil && err != io.EOF {
			t.Fatalf("Concurrent call failed: %v", err)
		}
	}

	// Give server time to process
	time.Sleep(200 * time.Millisecond)

	// Verify each worker had its own ID
	for i := 0; i < numWorkers; i++ {
		method := "worker.method" + string(rune('0'+i))
		expectedID := "worker-" + string(rune('0'+i))

		if val, ok := receivedIDs.Load(method); !ok || val != expectedID {
			t.Errorf("Expected %s=%s, got %v", method, expectedID, val)
		}
	}

	cancelServer()
	client.Close()
	<-serverDone
}
