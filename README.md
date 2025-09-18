# JSON-RPC 2.0 Library for Go

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://golang.org/doc/devel/release.html)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A comprehensive, production-ready JSON-RPC 2.0 implementation for Go with support for multiple transport protocols (HTTP, TCP, UDP).

## Features

- **Full JSON-RPC 2.0 Specification**: Complete implementation of [JSON-RPC 2.0](https://www.jsonrpc.org/specification)
- **Multiple Transports**: HTTP, TCP, and UDP support
- **Client & Server**: Both client and server implementations
- **Context Support**: Full context.Context integration for timeouts and cancellation
- **Thread-Safe**: All operations are thread-safe and concurrent-friendly
- **Type Safety**: Strong typing with validation and `json.RawMessage` for flexible parameter handling
- **Flexible Parameter Parsing**: Use `json.RawMessage` to parse parameters as arrays or objects based on your needs
- **Error Handling**: Comprehensive error handling with standard JSON-RPC error codes
- **Production Ready**: Battle-tested with extensive test coverage

## Quick Start

### Installation

```bash
go get ella.to/jsonrpc
```

### Basic HTTP Server

```go
package main

import (
    "context"
    "encoding/json"
    "log"

    "ella.to/jsonrpc"
)

func main() {
    // Define your RPC handlers
    handlers := map[string]jsonrpc.Handler{
        "math.add": func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
            // Parse parameters from json.RawMessage
            var params map[string]any
            if err := json.Unmarshal(req.Params, &params); err != nil {
                return jsonrpc.NewErrorResponse(jsonrpc.InvalidParams, "Invalid params", nil, req.ID)
            }

            a := params["a"].(float64)
            b := params["b"].(float64)
            return jsonrpc.NewResponse(a+b, req.ID)
        },
        "greet": func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
            // Alternative: Parse into a specific struct
            var params struct {
                Name string `json:"name"`
            }
            if err := json.Unmarshal(req.Params, &params); err != nil {
                return jsonrpc.NewErrorResponse(jsonrpc.InvalidParams, "Invalid params", nil, req.ID)
            }

            return jsonrpc.NewResponse("Hello, "+params.Name+"!", req.ID)
        },
    }

    // Create and start HTTP server
    server := jsonrpc.NewHttpServer("127.0.0.1:8080", "/rpc", handlers)
    log.Println("JSON-RPC server starting on http://127.0.0.1:8080/rpc")
    log.Fatal(server.ListenAndServe())
}
```

### Basic HTTP Client

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"

    "ella.to/jsonrpc"
)

func main() {
    // Create HTTP client
    httpClient := jsonrpc.NewHttpClient(&http.Client{})
    client := jsonrpc.NewClient(httpClient)
    err := client.Connect(context.Background(), "http://127.0.0.1:8080/rpc")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Make a simple call
    var result float64
    err = client.CallWithResult(context.Background(), "math.add",
        map[string]any{"a": 5, "b": 3}, &result)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("5 + 3 = %.0f\n", result) // Output: 5 + 3 = 8

    // Make another call
    var greeting string
    err = client.CallWithResult(context.Background(), "greet",
        map[string]any{"name": "World"}, &greeting)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(greeting) // Output: Hello, World!
}
```

## Parameter Handling with json.RawMessage

This library uses `json.RawMessage` for `Request.Params` and `Response.Result` fields, providing flexibility in how you handle parameters and results.

### Benefits

- **Type Flexibility**: Parse parameters as arrays, objects, or specific structs
- **Performance**: Avoid unnecessary conversions when you know the expected structure
- **Validation**: Check parameter structure before parsing
- **Memory Efficiency**: Delay parsing until needed

### Parsing Parameters

#### As a Map (Dynamic)

```go
func handler(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
    var params map[string]any
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return jsonrpc.NewErrorResponse(jsonrpc.InvalidParams, "Invalid params", nil, req.ID)
    }

    name := params["name"].(string)
    age := int(params["age"].(float64)) // JSON numbers are float64

    return jsonrpc.NewResponse(fmt.Sprintf("%s is %d years old", name, age), req.ID)
}
```

#### As a Struct (Typed)

```go
type GreetParams struct {
    Name string `json:"name"`
    Age  int    `json:"age"`
}

func handler(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
    var params GreetParams
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return jsonrpc.NewErrorResponse(jsonrpc.InvalidParams, "Invalid params", nil, req.ID)
    }

    return jsonrpc.NewResponse(fmt.Sprintf("%s is %d years old", params.Name, params.Age), req.ID)
}
```

#### As an Array (Positional Parameters)

```go
func mathHandler(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
    var params []float64
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return jsonrpc.NewErrorResponse(jsonrpc.InvalidParams, "Invalid params", nil, req.ID)
    }

    if len(params) < 2 {
        return jsonrpc.NewErrorResponse(jsonrpc.InvalidParams, "Need at least 2 numbers", nil, req.ID)
    }

    return jsonrpc.NewResponse(params[0] + params[1], req.ID)
}
```

#### Checking Parameter Type Before Parsing

```go
func flexibleHandler(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
    // Check if params is an array or object
    if len(req.Params) > 0 && req.Params[0] == '[' {
        // Handle array parameters
        var params []any
        if err := json.Unmarshal(req.Params, &params); err != nil {
            return jsonrpc.NewErrorResponse(jsonrpc.InvalidParams, "Invalid array params", nil, req.ID)
        }
        // Process array...
    } else {
        // Handle object parameters
        var params map[string]any
        if err := json.Unmarshal(req.Params, &params); err != nil {
            return jsonrpc.NewErrorResponse(jsonrpc.InvalidParams, "Invalid object params", nil, req.ID)
        }
        // Process object...
    }

    return jsonrpc.NewResponse("processed", req.ID)
}
```

### Working with Results

When making client calls, results are also `json.RawMessage`:

```go
// Using CallWithResult (automatic unmarshaling)
var result string
err := client.CallWithResult(ctx, "greet", params, &result)

// Using Call (manual unmarshaling)
resp, err := client.Call(ctx, "greet", params)
if err != nil {
    log.Fatal(err)
}

var result string
if err := json.Unmarshal(resp.Result, &result); err != nil {
    log.Fatal(err)
}
```

### Migration from `any` to `json.RawMessage`

If you're migrating from a version that used `any` for parameters:

#### Before (with `any`)

```go
func handler(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
    params := req.Params.(map[string]any)  // Direct type assertion
    name := params["name"].(string)
    return jsonrpc.NewResponse("Hello "+name, req.ID)
}
```

#### After (with `json.RawMessage`)

```go
func handler(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
    var params map[string]any
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return jsonrpc.NewErrorResponse(jsonrpc.InvalidParams, "Invalid params", nil, req.ID)
    }
    name := params["name"].(string)
    return jsonrpc.NewResponse("Hello "+name, req.ID)
}
```

The new approach provides:

- Better error handling for invalid parameters
- Type safety at parse time
- Flexibility to parse as different types
- No runtime panics from failed type assertions

## Transport Protocols

### HTTP Transport

Best for web applications and REST-like services.

```go
// Server
server := jsonrpc.NewHttpServer("127.0.0.1:8080", "/rpc", handlers)
server.ListenAndServe()

// Client
client := jsonrpc.NewHttpClient(&http.Client{})
client.Connect(context.Background(), "http://127.0.0.1:8080/rpc")
```

### TCP Transport

Best for persistent connections and high-performance applications.

```go
// Server
server, err := jsonrpc.NewTcpServer("127.0.0.1:8080")
if err != nil {
    log.Fatal(err)
}
defer server.Close()

for {
    codec, err := server.Accept(context.Background())
    if err != nil {
        continue
    }

    go handleConnection(codec) // Handle each connection in a goroutine
}

// Client
client := jsonrpc.NewTcpClient()
err := client.Connect(context.Background(), "127.0.0.1:8080")
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

### UDP Transport

Best for low-latency, stateless communication.

```go
// Server
server, err := jsonrpc.NewUdpServer("127.0.0.1:8080")
if err != nil {
    log.Fatal(err)
}
defer server.Close()

for {
    codec, err := server.Accept(context.Background())
    if err != nil {
        continue
    }

    go handleUDPRequest(codec) // Handle each request
}

// Client
client := jsonrpc.NewUdpClient()
err := client.Connect(context.Background(), "127.0.0.1:8080")
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

## Advanced Usage

### Custom Error Handling

```go
func mathDivide(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
    // Parse parameters with error handling
    var params map[string]any
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return jsonrpc.NewErrorResponse(
            jsonrpc.InvalidParams,
            "Invalid parameters",
            "Parameters must be a JSON object",
            req.ID,
        )
    }

    a, ok1 := params["a"].(float64)
    b, ok2 := params["b"].(float64)

    if !ok1 || !ok2 {
        return jsonrpc.NewErrorResponse(
            jsonrpc.InvalidParams,
            "Invalid parameter types",
            "Parameters 'a' and 'b' must be numbers",
            req.ID,
        )
    }

    if b == 0 {
        return jsonrpc.NewErrorResponse(
            jsonrpc.InvalidParams,
            "Division by zero",
            "Cannot divide by zero",
            req.ID,
        )
    }

    return jsonrpc.NewResponse(a/b, req.ID)
}
```

### Notifications (Fire-and-Forget)

```go
// Client sends a notification (no response expected)
err := client.Notify(context.Background(), "log.info",
    map[string]any{"message": "User logged in", "userID": 123})
```

### Batch Requests

```go
// Send multiple requests at once
requests := []*jsonrpc.Request{
    jsonrpc.NewRequest("math.add", map[string]any{"a": 1, "b": 2}, 1),
    jsonrpc.NewRequest("math.add", map[string]any{"a": 3, "b": 4}, 2),
}

responses, err := client.CallBatch(context.Background(), requests)
if err != nil {
    log.Fatal(err)
}

for _, resp := range responses {
    fmt.Printf("ID %v: Result = %v\n", resp.ID, resp.Result)
}
```

### Context with Timeout

```go
// Set a timeout for operations
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

var result string
err := client.CallWithResult(ctx, "slow.operation", nil, &result)
if err != nil {
    if err == context.DeadlineExceeded {
        fmt.Println("Operation timed out")
    } else {
        log.Fatal(err)
    }
}
```

### Middleware and Interceptors

```go
func loggingHandler(next jsonrpc.Handler) jsonrpc.Handler {
    return func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
        log.Printf("Calling method: %s", req.Method)

        result := next(ctx, req)

        log.Printf("Method %s completed", req.Method)
        return result
    }
}

// Wrap your handlers
handlers := map[string]jsonrpc.Handler{
    "greet": loggingHandler(greetHandler),
}
```

## Error Codes

The library includes standard JSON-RPC 2.0 error codes:

```go
const (
    ParseError     = -32700 // Invalid JSON was received
    InvalidRequest = -32600 // The JSON sent is not a valid Request object
    MethodNotFound = -32601 // The method does not exist / is not available
    InvalidParams  = -32602 // Invalid method parameter(s)
    InternalError  = -32603 // Internal JSON-RPC error
)
```

## Configuration

### HTTP Transport Options

```go
// Custom HTTP client with timeout
httpClient := &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        IdleConnTimeout:     90 * time.Second,
        DisableCompression:  true,
    },
}

client := jsonrpc.NewHttpClient(httpClient)
```

### UDP Transport Options

```go
// UDP client with custom timeout
client := jsonrpc.NewUdpClientWithTimeout(10 * time.Second)

// UDP server with custom timeout
server, err := jsonrpc.NewUdpServerWithTimeout("127.0.0.1:8080", 15 * time.Second)
```

## Testing

The library comes with comprehensive test coverage. Run tests with:

```bash
go test ./...
```

For verbose output:

```bash
go test -v ./...
```

## Performance Considerations

### Transport Selection

- **HTTP**: Best for web integration, supports load balancers, easy debugging
- **TCP**: Best for persistent connections, lower overhead than HTTP
- **UDP**: Best for low-latency scenarios, but no guarantee of delivery

### Connection Pooling

For HTTP clients, configure connection pooling:

```go
transport := &http.Transport{
    MaxIdleConns:        100,
    MaxIdleConnsPerHost: 10,
    IdleConnTimeout:     90 * time.Second,
}

client := &http.Client{Transport: transport}
jsonrpcClient := jsonrpc.NewHttpClient(client)
```

### Concurrent Connections

All transports support concurrent operations:

```go
// Safe to use from multiple goroutines
var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        var result float64
        client.CallWithResult(ctx, "math.add", params, &result)
    }()
}
wg.Wait()
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
