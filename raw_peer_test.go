package jsonrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"ella.to/jsonrpc"
	"ella.to/slogx"
)

type peerHarness struct {
	peer1       *jsonrpc.RawPeer
	peer2       *jsonrpc.RawPeer
	done1       chan error
	done2       chan error
	cancel      context.CancelFunc
	cleanupOnce sync.Once
}

func newPeerHarness(t *testing.T, handler1, handler2 jsonrpc.Handler) *peerHarness {
	t.Helper()

	peer1, peer2 := jsonrpc.NewRawPeerPair(handler1, handler2)

	ctx, cancel := context.WithCancel(context.Background())
	done1 := make(chan error, 1)
	done2 := make(chan error, 1)

	go func() {
		done1 <- peer1.Serve(ctx)
	}()

	go func() {
		done2 <- peer2.Serve(ctx)
	}()

	h := &peerHarness{
		peer1:  peer1,
		peer2:  peer2,
		done1:  done1,
		done2:  done2,
		cancel: cancel,
	}

	t.Cleanup(func() {
		h.cleanup(t)
	})

	return h
}

func (h *peerHarness) cleanup(t *testing.T) {
	h.cleanupOnce.Do(func() {
		_ = h.peer1.Close()
		_ = h.peer2.Close()
		h.cancel()

		timeout := time.After(time.Second)
		for i := 0; i < 2; i++ {
			select {
			case err := <-h.done1:
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
					t.Errorf("peer1 exited with error: %v", err)
				}
			case err := <-h.done2:
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
					t.Errorf("peer2 exited with error: %v", err)
				}
			case <-timeout:
				t.Errorf("peer shutdown timed out")
				return
			}
		}
	})
}

func TestPeerBasicCall(t *testing.T) {
	reqCh := make(chan jsonrpc.Request, 1)

	h := newPeerHarness(t,
		nil, // peer1 has no handler
		jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
			ctx = slogx.Context(ctx)
			reqCh <- *req
			return req.CreateResponse(map[string]string{"message": "pong"})
		}),
	)

	// peer1 calls peer2
	responses, err := h.peer1.Call(context.Background(), jsonrpc.WithRequest("echo", map[string]string{"message": "ping"}, false))
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected single response, got %d", len(responses))
	}
	resp := responses[0]
	if resp == nil {
		t.Fatalf("Call returned nil response")
	}
	if resp.Error != nil {
		t.Fatalf("Call returned JSON-RPC error: %+v", resp.Error)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	if result["message"] != "pong" {
		t.Fatalf("unexpected result payload: %+v", result)
	}

	received := recvPeerTimeout(t, reqCh, time.Second)
	if received.Method != "echo" {
		t.Fatalf("unexpected method: %s", received.Method)
	}
}

func TestPeerBidirectionalCalls(t *testing.T) {
	// Both peers can call each other
	h := newPeerHarness(t,
		jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
			ctx = slogx.Context(ctx)
			return req.CreateResponse("from-peer1")
		}),
		jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
			ctx = slogx.Context(ctx)
			return req.CreateResponse("from-peer2")
		}),
	)

	// peer1 calls peer2
	responses, err := h.peer1.Call(context.Background(), jsonrpc.WithRequest("test", nil, false))
	if err != nil {
		t.Fatalf("peer1->peer2 call returned error: %v", err)
	}
	var result1 string
	if err := json.Unmarshal(responses[0].Result, &result1); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	if result1 != "from-peer2" {
		t.Fatalf("unexpected result: %s", result1)
	}

	// peer2 calls peer1
	responses, err = h.peer2.Call(context.Background(), jsonrpc.WithRequest("test", nil, false))
	if err != nil {
		t.Fatalf("peer2->peer1 call returned error: %v", err)
	}
	var result2 string
	if err := json.Unmarshal(responses[0].Result, &result2); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	if result2 != "from-peer1" {
		t.Fatalf("unexpected result: %s", result2)
	}
}

func TestPeerNestedCall(t *testing.T) {
	// This is the key test: peer2's handler calls back to peer1 while processing
	// a request from peer1. This would deadlock with separate client/server.

	var peer2 *jsonrpc.RawPeer

	handler1 := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		ctx = slogx.Context(ctx)
		if req.Method == "getSecret" {
			return req.CreateResponse("secret-value")
		}
		return req.CreateErrorResponse(jsonrpc.NewError(jsonrpc.MethodNotFound, "not found"))
	})

	handler2 := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		ctx = slogx.Context(ctx)
		if req.Method == "processWithSecret" {
			// Call back to peer1 to get the secret - this would deadlock without proper handling
			responses, err := peer2.Call(ctx, jsonrpc.WithRequest("getSecret", nil, false))
			if err != nil {
				return req.CreateErrorResponse(err)
			}
			if responses[0].Error != nil {
				return req.CreateErrorResponse(responses[0].Error)
			}

			var secret string
			if err := json.Unmarshal(responses[0].Result, &secret); err != nil {
				return req.CreateErrorResponse(err)
			}

			return req.CreateResponse(map[string]string{
				"processed": "done",
				"secret":    secret,
			})
		}
		return req.CreateErrorResponse(jsonrpc.NewError(jsonrpc.MethodNotFound, "not found"))
	})

	h := newPeerHarness(t, handler1, handler2)
	peer2 = h.peer2

	// peer1 calls peer2's processWithSecret, which calls back to peer1's getSecret
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	responses, err := h.peer1.Call(ctx, jsonrpc.WithRequest("processWithSecret", nil, false))
	if err != nil {
		t.Fatalf("nested call returned error: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected single response, got %d", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("unexpected error: %+v", responses[0].Error)
	}

	var result map[string]string
	if err := json.Unmarshal(responses[0].Result, &result); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	if result["processed"] != "done" {
		t.Fatalf("unexpected processed value: %s", result["processed"])
	}
	if result["secret"] != "secret-value" {
		t.Fatalf("unexpected secret value: %s", result["secret"])
	}
}

func TestPeerDeepNestedCalls(t *testing.T) {
	// Test a deeper call chain: peer1 -> peer2 -> peer1 -> peer2
	depth := 0
	var mu sync.Mutex

	var peer1, peer2 *jsonrpc.RawPeer

	handler1 := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		ctx = slogx.Context(ctx)
		mu.Lock()
		currentDepth := depth
		depth++
		mu.Unlock()

		if req.Method == "chain" && currentDepth < 2 {
			// Continue the chain by calling peer2
			responses, err := peer1.Call(ctx, jsonrpc.WithRequest("chain", map[string]int{"depth": currentDepth + 1}, false))
			if err != nil {
				return req.CreateErrorResponse(err)
			}
			return &jsonrpc.Response{
				JSONRPC: jsonrpc.Version,
				Result:  responses[0].Result,
				Error:   responses[0].Error,
				ID:      req.ID,
			}
		}
		return req.CreateResponse(map[string]int{"final_depth": currentDepth})
	})

	handler2 := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		ctx = slogx.Context(ctx)
		mu.Lock()
		currentDepth := depth
		depth++
		mu.Unlock()

		if req.Method == "chain" && currentDepth < 3 {
			// Continue the chain by calling peer1
			responses, err := peer2.Call(ctx, jsonrpc.WithRequest("chain", map[string]int{"depth": currentDepth + 1}, false))
			if err != nil {
				return req.CreateErrorResponse(err)
			}
			return &jsonrpc.Response{
				JSONRPC: jsonrpc.Version,
				Result:  responses[0].Result,
				Error:   responses[0].Error,
				ID:      req.ID,
			}
		}
		return req.CreateResponse(map[string]int{"final_depth": currentDepth})
	})

	h := newPeerHarness(t, handler1, handler2)
	peer1 = h.peer1
	peer2 = h.peer2

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the chain from peer1 calling peer2
	responses, err := h.peer1.Call(ctx, jsonrpc.WithRequest("chain", map[string]int{"depth": 0}, false))
	if err != nil {
		t.Fatalf("deep nested call returned error: %v", err)
	}
	if responses[0].Error != nil {
		t.Fatalf("unexpected error: %+v", responses[0].Error)
	}

	var result map[string]int
	if err := json.Unmarshal(responses[0].Result, &result); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}

	// The chain should have gone through multiple hops
	if result["final_depth"] < 2 {
		t.Fatalf("expected deeper chain, got depth %d", result["final_depth"])
	}
}

func TestPeerConcurrentCalls(t *testing.T) {
	h := newPeerHarness(t,
		jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
			ctx = slogx.Context(ctx)

			time.Sleep(10 * time.Millisecond)
			return req.CreateResponse(req.Method)
		}),
		jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
			ctx = slogx.Context(ctx)
			time.Sleep(10 * time.Millisecond)
			return req.CreateResponse(req.Method)
		}),
	)

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	// Launch concurrent calls from peer1 to peer2
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := h.peer1.Call(ctx, jsonrpc.WithRequest("method", map[string]int{"n": n}, false))
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	// Launch concurrent calls from peer2 to peer1
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := h.peer2.Call(ctx, jsonrpc.WithRequest("method", map[string]int{"n": n}, false))
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent call failed: %v", err)
	}
}

func TestPeerNotification(t *testing.T) {
	notifyCh := make(chan jsonrpc.Request, 1)

	h := newPeerHarness(t,
		nil,
		jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
			ctx = slogx.Context(ctx)
			notifyCh <- *req
			return nil
		}),
	)

	// Send notification (no response expected)
	responses, err := h.peer1.Call(context.Background(), jsonrpc.WithRequest("notify", map[string]int{"value": 42}, true))
	if err != nil {
		t.Fatalf("notification returned error: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected single response slot, got %d", len(responses))
	}
	if responses[0] != nil {
		t.Fatalf("expected nil response for notification, got %+v", responses[0])
	}

	received := recvPeerTimeout(t, notifyCh, time.Second)
	if received.Method != "notify" {
		t.Fatalf("unexpected method: %s", received.Method)
	}
	if received.ID != nil {
		t.Fatalf("notification should not include ID")
	}
}

func TestPeerCallWithNilResultResponse(t *testing.T) {
	h := newPeerHarness(t,
		nil,
		jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
			ctx = slogx.Context(ctx)
			return req.CreateResponse(nil)
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	responses, err := h.peer1.Call(ctx, jsonrpc.WithRequest("delete", nil, false))
	if err != nil {
		t.Fatalf("call returned error: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0] == nil {
		t.Fatalf("expected non-nil response")
	}
	if responses[0].Error != nil {
		t.Fatalf("unexpected error response: %+v", responses[0].Error)
	}
	if string(responses[0].Result) != "null" {
		t.Fatalf("expected null result payload, got %q", string(responses[0].Result))
	}
}

func recvPeerTimeout[T any](t *testing.T, ch <-chan T, timeout time.Duration) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for value")
	}
	var zero T
	return zero
}
