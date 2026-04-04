package jsonrpc

import (
	"context"
	"encoding/json"
)

// contextKey is the type for context keys used in context propagation
type contextKey string

// ContextPropagator defines the interface for extracting and injecting context values
// across RPC boundaries. This allows specific context keys to be serialized on the
// client side and deserialized on the server side, working with both HTTP and raw transports.
type ContextPropagator interface {
	// Extract retrieves values from the context that should be propagated to the remote side.
	// The returned map will be serialized and sent with the request.
	Extract(ctx context.Context) map[string]string

	// Inject takes the propagated metadata and injects it back into the context on the remote side.
	// This is called on the server side to restore context values from the client.
	Inject(ctx context.Context, metadata map[string]string) context.Context
}

// DefaultContextPropagator is a simple implementation that propagates specific context keys.
// Users can register which keys they want to propagate by adding them via RegisterKey.
type DefaultContextPropagator struct {
	keys []contextKey
}

// NewDefaultContextPropagator creates a new DefaultContextPropagator with the given keys to propagate.
func NewDefaultContextPropagator(keys ...string) *DefaultContextPropagator {
	ctxKeys := make([]contextKey, len(keys))
	for i, k := range keys {
		ctxKeys[i] = contextKey(k)
	}
	return &DefaultContextPropagator{keys: ctxKeys}
}

// Extract retrieves values for the registered keys from the context.
func (p *DefaultContextPropagator) Extract(ctx context.Context) map[string]string {
	if ctx == nil || len(p.keys) == 0 {
		return nil
	}

	metadata := make(map[string]string)
	for _, key := range p.keys {
		if val := ctx.Value(key); val != nil {
			if strVal, ok := val.(string); ok {
				metadata[string(key)] = strVal
			}
		}
	}

	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

// Inject adds the propagated metadata back into the context.
func (p *DefaultContextPropagator) Inject(ctx context.Context, metadata map[string]string) context.Context {
	if len(metadata) == 0 {
		return ctx
	}

	for key, val := range metadata {
		ctx = context.WithValue(ctx, contextKey(key), val)
	}
	return ctx
}

// WithContextValue is a helper function to add a value to the context using a string key.
// This is useful for setting values that will be propagated via ContextPropagator.
func WithContextValue(ctx context.Context, key, value string) context.Context {
	return context.WithValue(ctx, contextKey(key), value)
}

// ContextValue retrieves a value from the context using a string key.
// This is the counterpart to WithContextValue.
func ContextValue(ctx context.Context, key string) (string, bool) {
	val := ctx.Value(contextKey(key))
	if val == nil {
		return "", false
	}
	strVal, ok := val.(string)
	return strVal, ok
}

// serializeMetadata converts a metadata map to JSON bytes.
// Used internally for raw transport.
func serializeMetadata(metadata map[string]string) ([]byte, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	return json.Marshal(metadata)
}

// deserializeMetadata converts JSON bytes back to a metadata map.
// Used internally for raw transport.
func deserializeMetadata(data []byte) (map[string]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var metadata map[string]string
	err := json.Unmarshal(data, &metadata)
	return metadata, err
}

// RequestWithMetadata wraps a Request with metadata for raw transport.
// This is used internally when context propagation is enabled.
type RequestWithMetadata struct {
	Request  *Request          `json:"request"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ToRequest converts RequestWithMetadata back to a regular Request.
func (r *RequestWithMetadata) ToRequest() *Request {
	return r.Request
}

// GetMetadata returns the metadata from the request.
func (r *RequestWithMetadata) GetMetadata() map[string]string {
	return r.Metadata
}
