package jsonrpc

import (
	"context"

	"ella.to/slogx"
)

// SlogxPropagator carries the slogx trace-id across JSON-RPC call boundaries.
// It implements ContextPropagator and works identically for both transports:
//   - HTTP  – the trace-id is forwarded via the "X-Rpc-Meta-_traceId" request header.
//   - Raw   – the trace-id is embedded in the metadata envelope that the raw
//     client wraps around the JSON-RPC request array.
//
// On the server side, Inject restores the original trace-id and opens a new
// slogx span so that every log record emitted by the handler shows up as a
// child node of the client-side trace in the UI.
//
// # Setup
//
//	// Server (HTTP):
//	http.Handle("/rpc", jsonrpc.NewHTTPHandler(myHandler,
//	    jsonrpc.WithContextPropagation(jsonrpc.NewSlogxPropagator()),
//	))
//
//	// Client (HTTP):
//	client := jsonrpc.NewHTTPClient("http://host/rpc",
//	    jsonrpc.WithContextPropagation(jsonrpc.NewSlogxPropagator()),
//	)
//
//	// For raw (peer-to-peer) transport substitute NewRawServer / NewRawClient.
type SlogxPropagator struct{}

// NewSlogxPropagator returns a ready-to-use SlogxPropagator.
// Pass it to WithContextPropagation on both the client and the server.
func NewSlogxPropagator() *SlogxPropagator {
	return &SlogxPropagator{}
}

const slogxTraceIDKey = "_traceId"

// Extract reads the current slogx trace-id from ctx and returns it as a
// one-entry map so the jsonrpc transport can forward it to the remote side.
// Returns nil when no trace is active (the server will start a fresh one).
func (p *SlogxPropagator) Extract(ctx context.Context) map[string]string {
	id := slogx.TraceID(ctx)
	if id == "" {
		return nil
	}
	return map[string]string{slogxTraceIDKey: id}
}

// Inject takes the metadata map forwarded by the client, restores the
// trace-id into ctx (so all logs share the same trace), and then opens a
// new slogx span for the handler scope. If no trace-id is present (e.g. the
// client does not use slogx), a fresh trace is started automatically by the
// subsequent Context call.
func (p *SlogxPropagator) Inject(ctx context.Context, metadata map[string]string) context.Context {
	if id, ok := metadata[slogxTraceIDKey]; ok && id != "" {
		ctx = slogx.WithTraceID(ctx, id)
	}
	return slogx.Context(ctx)
}
