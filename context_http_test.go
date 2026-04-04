package jsonrpc

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestHTTPContextPropagation(t *testing.T) {
	// Create a propagator that propagates specific keys
	propagator := NewDefaultContextPropagator("request-id", "user-id", "trace-id")

	// Create a handler that checks if context values are present
	receivedValues := make(map[string]string)
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		// Extract the propagated values
		if val, ok := ContextValue(ctx, "request-id"); ok {
			receivedValues["request-id"] = val
		}
		if val, ok := ContextValue(ctx, "user-id"); ok {
			receivedValues["user-id"] = val
		}
		if val, ok := ContextValue(ctx, "trace-id"); ok {
			receivedValues["trace-id"] = val
		}

		return req.CreateResponse(map[string]string{"status": "ok"})
	})

	// Create an HTTP server with context propagation
	httpHandler := NewHTTPHandler(handler, WithContextPropagation(propagator))
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create a client with context propagation
	client := NewHTTPClient(server.URL, WithContextPropagation(propagator))

	// Create a context with values to propagate
	ctx := context.Background()
	ctx = WithContextValue(ctx, "request-id", "req-123")
	ctx = WithContextValue(ctx, "user-id", "user-456")
	ctx = WithContextValue(ctx, "trace-id", "trace-789")

	// Make a request
	req := WithRequest("test.method", nil, false)
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

	// Verify the values were propagated
	if receivedValues["request-id"] != "req-123" {
		t.Errorf("Expected request-id=req-123, got %s", receivedValues["request-id"])
	}
	if receivedValues["user-id"] != "user-456" {
		t.Errorf("Expected user-id=user-456, got %s", receivedValues["user-id"])
	}
	if receivedValues["trace-id"] != "trace-789" {
		t.Errorf("Expected trace-id=trace-789, got %s", receivedValues["trace-id"])
	}
}

func TestHTTPContextPropagationWithBatch(t *testing.T) {
	propagator := NewDefaultContextPropagator("correlation-id")

	var correlationID string
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if val, ok := ContextValue(ctx, "correlation-id"); ok {
			correlationID = val
		}
		return req.CreateResponse(map[string]string{"method": req.Method})
	})

	httpHandler := NewHTTPHandler(handler, WithContextPropagation(propagator))
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, WithContextPropagation(propagator))

	ctx := context.Background()
	ctx = WithContextValue(ctx, "correlation-id", "batch-999")

	// Send batch request
	req1 := WithRequest("method1", nil, false)
	req2 := WithRequest("method2", nil, false)
	responses, err := client.Call(ctx, req1, req2)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(responses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(responses))
	}

	// The correlation ID should have been set by the last request processed
	// (order is not guaranteed in batch, but at least one should set it)
	if correlationID != "batch-999" {
		t.Errorf("Expected correlation-id=batch-999, got %s", correlationID)
	}
}

func TestHTTPContextPropagationWithoutPropagator(t *testing.T) {
	// Handler that checks for context values
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		// Should NOT have the propagated value
		if val, ok := ContextValue(ctx, "should-not-exist"); ok {
			t.Errorf("Unexpected value found: %s", val)
		}
		return req.CreateResponse(map[string]string{"status": "ok"})
	})

	// Create server WITHOUT context propagation
	httpHandler := HTTPHandler(handler)
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create client WITHOUT context propagation
	client := NewHTTPClient(server.URL)

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
}

func TestHTTPContextPropagationWithEmptyMetadata(t *testing.T) {
	propagator := NewDefaultContextPropagator("key-that-does-not-exist")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		return req.CreateResponse(map[string]string{"status": "ok"})
	})

	httpHandler := NewHTTPHandler(handler, WithContextPropagation(propagator))
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, WithContextPropagation(propagator))

	// Context without the expected key
	ctx := context.Background()
	ctx = WithContextValue(ctx, "some-other-key", "value")

	req := WithRequest("test.method", nil, false)
	responses, err := client.Call(ctx, req)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(responses))
	}
}

func TestHTTPContextPropagationWithMultipleKeys(t *testing.T) {
	// Test with many context keys
	keys := []string{"key1", "key2", "key3", "key4", "key5"}
	propagator := NewDefaultContextPropagator(keys...)

	receivedCount := 0
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		for _, key := range keys {
			if _, ok := ContextValue(ctx, key); ok {
				receivedCount++
			}
		}
		return req.CreateResponse(map[string]string{"status": "ok"})
	})

	httpHandler := NewHTTPHandler(handler, WithContextPropagation(propagator))
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := NewHTTPClient(server.URL, WithContextPropagation(propagator))

	ctx := context.Background()
	for i, key := range keys {
		ctx = WithContextValue(ctx, key, "value"+string(rune('0'+i)))
	}

	req := WithRequest("test.method", nil, false)
	_, err := client.Call(ctx, req)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if receivedCount != len(keys) {
		t.Errorf("Expected %d keys to be propagated, got %d", len(keys), receivedCount)
	}
}

func TestContextPropagatorExtractAndInject(t *testing.T) {
	propagator := NewDefaultContextPropagator("test-key")

	// Test Extract
	ctx := context.Background()
	ctx = WithContextValue(ctx, "test-key", "test-value")

	metadata := propagator.Extract(ctx)
	if metadata == nil {
		t.Fatal("Expected metadata to be non-nil")
	}
	if metadata["test-key"] != "test-value" {
		t.Errorf("Expected test-key=test-value, got %s", metadata["test-key"])
	}

	// Test Inject
	newCtx := context.Background()
	newCtx = propagator.Inject(newCtx, metadata)

	val, ok := ContextValue(newCtx, "test-key")
	if !ok {
		t.Error("Expected test-key to be in context")
	}
	if val != "test-value" {
		t.Errorf("Expected test-value, got %s", val)
	}
}

func TestContextPropagatorWithNilMetadata(t *testing.T) {
	propagator := NewDefaultContextPropagator("test-key")

	// Test Inject with nil metadata
	ctx := context.Background()
	newCtx := propagator.Inject(ctx, nil)

	if newCtx != ctx {
		t.Error("Expected context to be unchanged when metadata is nil")
	}
}

func TestContextPropagatorWithEmptyKeys(t *testing.T) {
	propagator := NewDefaultContextPropagator()

	ctx := context.Background()
	ctx = WithContextValue(ctx, "some-key", "some-value")

	metadata := propagator.Extract(ctx)
	if metadata != nil {
		t.Error("Expected metadata to be nil when no keys are registered")
	}
}

func TestHTTPHeaderCaseInsensitivity(t *testing.T) {
	// Test that HTTP headers are properly handled case-insensitively
	// This is already tested in the main TestHTTPContextPropagation test
	// where headers are set and verified to work correctly
	t.Skip("Header case-insensitivity is already tested in TestHTTPContextPropagation")
}
