package jsonrpc

import (
	"bytes"
	"context"
	"ella.to/slogx"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// HTTPClient issues JSON-RPC requests over HTTP.
type HTTPClient struct {
	endpoint          string
	client            *http.Client
	withTrace         bool
	Headers           map[string]string
	contextPropagator ContextPropagator
}

var _ Caller = (*HTTPClient)(nil)

type HttpClientOpt interface {
	configureHttpClient(*HTTPClient) error
}

type HttpClientOptFunc func(*HTTPClient) error

func (f HttpClientOptFunc) configureHttpClient(c *HTTPClient) error {
	return f(c)
}

// NewHTTPClient constructs an HTTP JSON-RPC client targeting endpoint. If client is nil,
// http.DefaultClient is used. Options can include WithContextPropagation.
func NewHTTPClient(endpoint string, opts ...HttpClientOpt) *HTTPClient {
	if endpoint == "" {
		panic("jsonrpc2: endpoint cannot be empty")
	}

	httpClient := &HTTPClient{
		endpoint: endpoint,
		client:   http.DefaultClient,
		Headers:  map[string]string{},
	}

	// Apply options
	for _, opt := range opts {
		if err := opt.configureHttpClient(httpClient); err != nil {
			panic(err)
		}
	}

	return httpClient
}

// WithHeader adds a header to be sent on each request.
func (c *HTTPClient) WithHeader(key, value string) *HTTPClient {
	if c.Headers == nil {
		c.Headers = map[string]string{}
	}
	c.Headers[key] = value
	return c
}

// Call sends the provided requests to the JSON-RPC endpoint. The payload is always
// encoded as a JSON array, even when only a single request is supplied. Responses are
// aligned with the order of the supplied requests, and notifications (requests without
// an id) yield nil entries in the returned slice.
func (c *HTTPClient) Call(ctx context.Context, requests ...*Request) ([]*Response, error) {
	ctx = slogx.Context(ctx)
	if c == nil {
		return nil, fmt.Errorf("jsonrpc2: HTTPClient is nil")
	}
	if len(requests) == 0 {
		return nil, nil
	}

	responses := make([]*Response, len(requests))
	idToIndex := make(map[string]int, len(requests))
	expected := 0

	for i, req := range requests {
		if req == nil {
			return nil, fmt.Errorf("jsonrpc2: request at index %d is nil", i)
		}
		if req.JSONRPC == "" {
			req.JSONRPC = Version
		}
		if req.ID == nil {
			continue
		}
		idKey, ok := NormalizeID(req.ID)
		if !ok {
			return nil, fmt.Errorf("jsonrpc2: invalid request id at index %d", i)
		}
		if _, exists := idToIndex[idKey]; exists {
			return nil, fmt.Errorf("jsonrpc2: duplicate request id %s", idKey)
		}
		idToIndex[idKey] = i
		expected++
	}

	body, status, err := c.send(ctx, requests)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("jsonrpc2: unexpected HTTP status %d", status)
	}
	if expected == 0 {
		return responses, nil
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("jsonrpc2: empty response body")
	}

	switch trimmed[0] {
	case '[':
		var payload []Response
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return nil, err
		}
		for i := range payload {
			resp := payload[i]
			if resp.JSONRPC != Version {
				return nil, fmt.Errorf("jsonrpc2: invalid JSON-RPC version %q", resp.JSONRPC)
			}
			idKey, ok := NormalizeID(resp.ID)
			if !ok {
				continue
			}
			idx, ok := idToIndex[idKey]
			if !ok {
				continue
			}
			respCopy := resp
			responses[idx] = &respCopy
		}
	case '{':
		var resp Response
		if err := json.Unmarshal(trimmed, &resp); err != nil {
			return nil, err
		}
		if resp.JSONRPC != Version {
			return nil, fmt.Errorf("jsonrpc2: invalid JSON-RPC version %q", resp.JSONRPC)
		}
		idKey, ok := NormalizeID(resp.ID)
		if !ok {
			return nil, fmt.Errorf("jsonrpc2: response missing id")
		}
		idx, ok := idToIndex[idKey]
		if !ok {
			return nil, fmt.Errorf("jsonrpc2: unexpected response id %s", idKey)
		}
		respCopy := resp
		responses[idx] = &respCopy
	default:
		return nil, fmt.Errorf("jsonrpc2: invalid response payload")
	}

	for _, idx := range idToIndex {
		if responses[idx] == nil {
			return nil, fmt.Errorf("jsonrpc2: missing response for request index %d", idx)
		}
	}

	return responses, nil
}

func (c *HTTPClient) send(ctx context.Context, requests []*Request) ([]byte, int, error) {
	ctx = slogx.Context(ctx)
	data, err := json.Marshal(requests)
	if err != nil {
		return nil, 0, err
	}

	endpoint := c.endpoint

	if c.withTrace {
		u, err := url.Parse(c.endpoint)
		if err != nil {
			return nil, 0, err
		}

		q := u.Query()
		for _, req := range requests {
			q.Add("ella_t", req.Method)
		}

		u.RawQuery = q.Encode()
		endpoint = u.String()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Apply custom headers
	for key, value := range c.Headers {
		req.Header.Set(key, value)
	}

	// Extract and propagate context metadata as HTTP headers
	if c.contextPropagator != nil {
		metadata := c.contextPropagator.Extract(ctx)
		for key, value := range metadata {
			req.Header.Set("X-Rpc-Meta-"+key, value)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}
