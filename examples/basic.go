// Copyright (c) 2025 JSON-RPC v2 Contributors
// Licensed under the MIT License. See LICENSE file in the project root for full license information.

// Package main demonstrates a simple HTTP JSON-RPC server and client.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"ella.to/jsonrpc"
)

func main() {
	// Start server in a separate goroutine
	go startServer()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Run client
	runClient()
}

func startServer() {
	handlers := map[string]jsonrpc.Handler{
		"math.add": func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
			params := req.Params.(map[string]any)
			a := params["a"].(float64)
			b := params["b"].(float64)
			return jsonrpc.NewResponse(a+b, req.ID)
		},
		"greet": func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
			params := req.Params.(map[string]any)
			name := params["name"].(string)
			return jsonrpc.NewResponse("Hello, "+name+"!", req.ID)
		},
	}

	server := jsonrpc.NewHttpServer("127.0.0.1:8080", "/rpc", handlers)
	log.Println("JSON-RPC server starting on http://127.0.0.1:8080/rpc")
	log.Fatal(server.ListenAndServe())
}

func runClient() {
	// Create HTTP client codec
	httpClient := jsonrpc.NewHttpClient(&http.Client{})

	// Create high-level client with the HTTP codec
	client := jsonrpc.NewClient(httpClient)
	err := client.Connect(context.Background(), "http://127.0.0.1:8080/rpc")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// Example 1: Math operation
	var result float64
	err = client.CallWithResult(context.Background(), "math.add",
		map[string]any{"a": 5, "b": 3}, &result)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("5 + 3 = %.0f\n", result)

	// Example 2: Greeting
	var greeting string
	err = client.CallWithResult(context.Background(), "greet",
		map[string]any{"name": "World"}, &greeting)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(greeting)

	// Example 3: Using raw Call method
	response, err := client.Call(context.Background(), "math.add",
		map[string]any{"a": 10, "b": 20})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("10 + 20 = %.0f\n", response.Result.(float64))

	fmt.Println("Examples completed successfully!")
}
