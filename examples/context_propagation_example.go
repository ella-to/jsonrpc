package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http/httptest"
	"time"

	"ella.to/jsonrpc"
	"ella.to/slogx"
)

// demonstrateHTTPContextPropagation shows how to use context propagation with HTTP transport
func demonstrateHTTPContextPropagation() {
	fmt.Println("\n=== HTTP Transport Context Propagation ===")

	// Create a propagator for specific context keys
	propagator := jsonrpc.NewDefaultContextPropagator("request-id", "user-id")

	// Create a handler that uses propagated context
	handler := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		ctx = slogx.Context(ctx)

		requestID, _ := jsonrpc.ContextValue(ctx, "request-id")
		userID, _ := jsonrpc.ContextValue(ctx, "user-id")

		fmt.Printf("  Server received - Method: %s, Request ID: %s, User ID: %s\n",
			req.Method, requestID, userID)

		return req.CreateResponse(map[string]string{
			"message":    "Hello from server",
			"request-id": requestID,
		})
	})

	// Create HTTP server with context propagation
	httpHandler := jsonrpc.NewHTTPHandler(handler, jsonrpc.WithContextPropagation(propagator))
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	// Create HTTP client with context propagation
	client := jsonrpc.NewHTTPClient(server.URL, jsonrpc.WithContextPropagation(propagator))
	// Context propagation is now set via NewHTTPClient options

	// Create context with values to propagate
	ctx := context.Background()
	ctx = jsonrpc.WithContextValue(ctx, "request-id", "http-req-12345")
	ctx = jsonrpc.WithContextValue(ctx, "user-id", "user-alice")

	fmt.Println("  Client sending request with context values...")

	// Make the call
	req := jsonrpc.WithRequest("greet", map[string]string{"name": "Alice"}, false)
	responses, err := client.Call(ctx, req)
	if err != nil {
		log.Fatalf("Call failed: %v", err)
	}

	if len(responses) > 0 && responses[0].Error == nil {
		fmt.Printf("  Client received response: %s\n", string(responses[0].Result))
	}
}

// demonstrateRawContextPropagation shows how to use context propagation with raw transport
func demonstrateRawContextPropagation() {
	fmt.Println("\n=== Raw Transport Context Propagation ===")

	// Create a propagator for specific context keys
	propagator := jsonrpc.NewDefaultContextPropagator("trace-id", "session-id")

	// Create a handler that uses propagated context
	handler := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		ctx = slogx.Context(ctx)

		traceID, _ := jsonrpc.ContextValue(ctx, "trace-id")
		sessionID, _ := jsonrpc.ContextValue(ctx, "session-id")

		fmt.Printf("  Server received - Method: %s, Trace ID: %s, Session ID: %s\n",
			req.Method, traceID, sessionID)

		return req.CreateResponse(map[string]string{
			"message":  "Hello from server",
			"trace-id": traceID,
		})
	})

	// Create a pipe for communication
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Start server in background
	server := jsonrpc.NewRawServer(serverConn, handler, jsonrpc.WithContextPropagation(propagator))
	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	go func() {
		if err := server.Serve(serverCtx); err != nil {
			fmt.Printf("  Server error: %v\n", err)
		}
	}()

	// Create client with context propagation
	client := jsonrpc.NewRawClient(clientConn, jsonrpc.WithContextPropagation(propagator))
	defer client.Close()

	// Create context with values to propagate
	ctx := context.Background()
	ctx = jsonrpc.WithContextValue(ctx, "trace-id", "trace-xyz-789")
	ctx = jsonrpc.WithContextValue(ctx, "session-id", "session-bob-456")

	fmt.Println("  Client sending request with context values...")

	// Make the call
	req := jsonrpc.WithRequest("process", map[string]string{"data": "important"}, false)
	responses, err := client.Call(ctx, req)
	if err != nil {
		log.Fatalf("Call failed: %v", err)
	}

	if len(responses) > 0 && responses[0].Error == nil {
		fmt.Printf("  Client received response: %s\n", string(responses[0].Result))
	}

	// Give server time to process
	time.Sleep(100 * time.Millisecond)
}

// demonstrateBatchWithContext shows batch requests with context propagation
func demonstrateBatchWithContext() {
	fmt.Println("\n=== Batch Requests with Context Propagation ===")

	propagator := jsonrpc.NewDefaultContextPropagator("batch-id")

	handler := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		ctx = slogx.Context(ctx)
		batchID, _ := jsonrpc.ContextValue(ctx, "batch-id")
		fmt.Printf("  Server processing %s with batch ID: %s\n", req.Method, batchID)

		return req.CreateResponse(map[string]string{
			"method":   req.Method,
			"batch-id": batchID,
		})
	})

	httpHandler := jsonrpc.NewHTTPHandler(handler, jsonrpc.WithContextPropagation(propagator))
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := jsonrpc.NewHTTPClient(server.URL, jsonrpc.WithContextPropagation(propagator))
	// Context propagation is now set via NewHTTPClient options

	ctx := context.Background()
	ctx = jsonrpc.WithContextValue(ctx, "batch-id", "batch-999")

	fmt.Println("  Client sending batch request...")

	// Send batch request
	req1 := jsonrpc.WithRequest("method1", nil, false)
	req2 := jsonrpc.WithRequest("method2", nil, false)
	req3 := jsonrpc.WithRequest("method3", nil, false)

	responses, err := client.Call(ctx, req1, req2, req3)
	if err != nil {
		log.Fatalf("Batch call failed: %v", err)
	}

	fmt.Printf("  Client received %d responses\n", len(responses))
}

// Custom propagator that adds timestamps
type TimestampPropagator struct {
	*jsonrpc.DefaultContextPropagator
}

func (p *TimestampPropagator) Extract(ctx context.Context) map[string]string {
	ctx = slogx.Context(ctx)
	metadata := p.DefaultContextPropagator.Extract(ctx)
	if metadata == nil {
		metadata = make(map[string]string)
	}
	// Add timestamp automatically
	metadata["client-timestamp"] = time.Now().Format(time.RFC3339)
	return metadata
}

// demonstrateCustomPropagator shows how to implement a custom propagator
func demonstrateCustomPropagator() {
	fmt.Println("\n=== Custom Context Propagator ===")

	basePropagator := jsonrpc.NewDefaultContextPropagator("request-id")
	propagator := &TimestampPropagator{DefaultContextPropagator: basePropagator}

	handler := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		ctx = slogx.Context(ctx)
		requestID, _ := jsonrpc.ContextValue(ctx, "request-id")
		timestamp, _ := jsonrpc.ContextValue(ctx, "client-timestamp")

		fmt.Printf("  Server received request ID: %s at client time: %s\n", requestID, timestamp)
		return req.CreateResponse(nil)
	})

	httpHandler := jsonrpc.NewHTTPHandler(handler, jsonrpc.WithContextPropagation(propagator))
	server := httptest.NewServer(httpHandler)
	defer server.Close()

	client := jsonrpc.NewHTTPClient(server.URL, jsonrpc.WithContextPropagation(propagator))
	// Context propagation is now set via NewHTTPClient options

	ctx := context.Background()
	ctx = jsonrpc.WithContextValue(ctx, "request-id", "custom-123")

	fmt.Println("  Client sending request (timestamp will be added automatically)...")

	req := jsonrpc.WithRequest("test", nil, false)
	_, err := client.Call(ctx, req)
	if err != nil {
		log.Fatalf("Call failed: %v", err)
	}
}

func main() {
	fmt.Println("Context Propagation Examples")
	fmt.Println("=============================")

	demonstrateHTTPContextPropagation()
	demonstrateRawContextPropagation()
	demonstrateBatchWithContext()
	demonstrateCustomPropagator()

	fmt.Println("\n✓ All examples completed successfully")
}
