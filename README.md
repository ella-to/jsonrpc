```
░░░░░██╗░██████╗░█████╗░███╗░░██╗██████╗░██████╗░░█████╗░
░░░░░██║██╔════╝██╔══██╗████╗░██║██╔══██╗██╔══██╗██╔══██╗
░░░░░██║╚█████╗░██║░░██║██╔██╗██║██████╔╝██████╔╝██║░░╚═╝
██╗░░██║░╚═══██╗██║░░██║██║╚████║██╔══██╗██╔═══╝░██║░░██╗
╚█████╔╝██████╔╝╚█████╔╝██║░╚███║██║░░██║██║░░░░░╚█████╔╝
░╚════╝░╚═════╝░░╚════╝░╚═╝░░╚══╝╚═╝░░╚═╝╚═╝░░░░░░╚════╝░
```
<div align="center">

[![Go Reference](https://pkg.go.dev/badge/ella.to/jsonrpc.svg)](https://pkg.go.dev/ella.to/jsonrpc)
[![Go Report Card](https://goreportcard.com/badge/ella.to/jsonrpc)](https://goreportcard.com/report/ella.to/jsonrpc)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**jsonrpc** is a JSON-RPC 2.0 implementation for Go that works over HTTP and raw sockets (`io.ReadWriteCloser`), with built-in batching, context propagation, and bidirectional peer support.

</div>

## Installation

```bash
go get ella.to/jsonrpc@v0.0.3
```

## Overview

This package gives you four things:

- **HTTPClient / HTTPHandler** — JSON-RPC over standard HTTP requests
- **RawClient / RawServer** — JSON-RPC over any `io.ReadWriteCloser` (TCP, Unix sockets, pipes, etc.)
- **RawPeer** — bidirectional JSON-RPC where both sides can send and receive (no deadlocks, even with nested calls)
- **Context Propagation** — pass arbitrary key/value metadata across RPC boundaries

All transports handle batching natively — you can send multiple requests in a single round trip.

## HTTP Transport

### Server

Wrap your handler with `NewHTTPHandler` and mount it on any `http.ServeMux`:

```go
handler := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
    switch req.Method {
    case "add":
        var params struct{ A, B int }
        json.Unmarshal(req.Params, &params)
        return req.CreateResponse(params.A + params.B)
    default:
        return req.CreateErrorResponse(jsonrpc.NewError(jsonrpc.MethodNotFound, "unknown method"))
    }
})

mux := http.NewServeMux()
mux.Handle("/rpc", jsonrpc.NewHTTPHandler(handler))
http.ListenAndServe(":8080", mux)
```

### Client

```go
client := jsonrpc.NewHTTPClient("http://localhost:8080/rpc")

req := jsonrpc.WithRequest("add", map[string]int{"A": 5, "B": 3}, false)
responses, err := client.Call(context.Background(), req)

var result int
json.Unmarshal(responses[0].Result, &result)
fmt.Println(result) // 8
```

### Batch Requests

Send multiple calls at once:

```go
responses, err := client.Call(ctx,
    jsonrpc.WithRequest("add", map[string]int{"A": 1, "B": 2}, false),
    jsonrpc.WithRequest("add", map[string]int{"A": 3, "B": 4}, false),
)
// responses[0] -> 3
// responses[1] -> 7
```

### Notifications

A notification is a request with no expected response. Pass `true` as the third argument:

```go
client.Call(ctx, jsonrpc.WithRequest("log", map[string]string{"msg": "hello"}, true))
```

## Raw Socket Transport

For persistent connections over TCP, Unix sockets, or anything that implements `io.ReadWriteCloser`.

### Server

```go
server := jsonrpc.NewRawServer(conn, handler)
err := server.Serve(context.Background()) // blocks until connection closes
```

### Client

```go
client := jsonrpc.NewRawClient(conn)

responses, err := client.Call(ctx,
    jsonrpc.WithRequest("getUser", map[string]string{"id": "abc"}, false),
)
```

## Bidirectional Peers

`RawPeer` lets both sides of a connection send requests to each other. This is the interesting one — a handler on one side can make calls back to the other side without deadlocking.

```go
// Create a pair of connected peers (useful for testing)
peerA, peerB := jsonrpc.NewRawPeerPair(handlerA, handlerB)

go peerA.Serve(ctx)
go peerB.Serve(ctx)

// peerA can call peerB
responses, _ := peerA.Call(ctx, jsonrpc.WithRequest("hello", nil, false))

// peerB can call peerA
responses, _ := peerB.Call(ctx, jsonrpc.WithRequest("world", nil, false))
```

In real usage, create a single peer from any `io.ReadWriteCloser`:

```go
peer := jsonrpc.NewRawPeer(conn, handler)
go peer.Serve(ctx)

// Now you can both handle incoming requests AND make outgoing calls
responses, err := peer.Call(ctx, jsonrpc.WithRequest("method", params, false))
```

Nested calls are safe — if peer A calls peer B, and B's handler calls back into A, it all works without deadlocking.

## Context Propagation

Pass metadata (like trace IDs or user info) across RPC calls. Over HTTP, values travel as `X-Rpc-Meta-*` headers. Over raw sockets, they're embedded in the request JSON.

```go
// Set up propagation for specific keys
propagator := jsonrpc.NewDefaultContextPropagator("trace-id", "user-id")

// Client side
client := jsonrpc.NewHTTPClient(url, jsonrpc.WithContextPropagation(propagator))

ctx := jsonrpc.WithContextValue(context.Background(), "trace-id", "abc-123")
ctx = jsonrpc.WithContextValue(ctx, "user-id", "alice")

responses, err := client.Call(ctx, jsonrpc.WithRequest("getProfile", nil, false))

// Server side — values are automatically injected into the handler's context
handler := jsonrpc.NewHTTPHandler(
    jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
        traceID, _ := jsonrpc.ContextValue(ctx, "trace-id") // "abc-123"
        userID, _ := jsonrpc.ContextValue(ctx, "user-id")   // "alice"
        // ...
    }),
    jsonrpc.WithContextPropagation(propagator),
)
```

## Error Handling

The package defines the standard JSON-RPC error codes:

| Code | Constant | Meaning |
|------|----------|---------|
| -32700 | `ParseError` | Invalid JSON |
| -32600 | `InvalidRequest` | Not a valid JSON-RPC request |
| -32601 | `MethodNotFound` | Method doesn't exist |
| -32602 | `InvalidParams` | Invalid parameters |
| -32603 | `InternalError` | Internal server error |

Create errors with optional cause chaining:

```go
err := jsonrpc.NewError(jsonrpc.InvalidParams, "name is required")

// With a wrapped cause
err := jsonrpc.NewError(jsonrpc.InternalError, "db failure").WithCause(dbErr)
```

Errors support `errors.Is()` and `errors.Unwrap()` for standard Go error patterns.

## Client Options

```go
// Custom HTTP client
client := jsonrpc.NewHTTPClient(url, jsonrpc.WithHttpClient(customClient))

// Trace mode (appends each request's method name to the URL as an
// `ella_t` query parameter, useful for spotting calls in access logs)
client := jsonrpc.NewHTTPClient(url, jsonrpc.WithTrace(true))

// Custom headers
client := jsonrpc.NewHTTPClient(url).WithHeader("Authorization", "Bearer token")
```

## License

MIT — see [LICENSE](LICENSE) for details.