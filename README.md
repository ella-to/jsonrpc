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
- **Type Safety**: Strong typing with validation
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
    "log"

    "ella.to/jsonrpc"
)

func main() {
    // Define your RPC handlers
    handlers := map[string]jsonrpc.Handler{
        "math.add": func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
            params := req.Params.(map[string]any)
            a := params["a"].(float64)
            b := params["b"].(float64)
            return jsonrpc.NewResponse(a+b, request.ID)
        },
        "greet": func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
            params := req.Params.(map[string]any)
            name := params["name"].(string)
            return jsonrpc.NewResponse("Hello, "+name+"!", request.ID)
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
    params := req.Params.(map[string]any)

    a := params["a"].(float64)
    b := params["b"].(float64)

    if b == 0 {
        return jsonrpc.NewErrorResponse(
            jsonrpc.InvalidParams,
            "Division by zero",
            "Cannot divide by zero",
            request.ID,
        )
    }

    return jsonrpc.NewResponse(a/b, request.ID)
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
        log.Printf("Calling method: %s", request.Method)

        result := next(ctx, req)

        log.Printf("Method %s completed", request.Method)
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

## Examples

Check out the `/examples` directory for complete working examples:

- [Basic HTTP Server/Client](examples/http)
- [TCP Server/Client](examples/tcp)
- [UDP Server/Client](examples/udp)
- [Advanced Features](examples/advanced)

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull requests.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for a detailed list of changes and version history.

## Support

- 📖 [Documentation](https://pkg.go.dev/ella.to/jsonrpc)
- 🐛 [Issue Tracker](https://github.com/your-repo/jsonrpc-v2/issues)
- 💬 [Discussions](https://github.com/your-repo/jsonrpc-v2/discussions)

## Acknowledgments

- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)
- Go community for excellent feedback and contributions
