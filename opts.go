package jsonrpc

import (
	"errors"
	"net/http"
)

type contextPropagatorOpt struct {
	propagator ContextPropagator
}

var (
	_ HttpClientOpt  = (*contextPropagatorOpt)(nil)
	_ RawClientOpt   = (*contextPropagatorOpt)(nil)
	_ HttpHandlerOpt = (*contextPropagatorOpt)(nil)
	_ RawServerOpt   = (*contextPropagatorOpt)(nil)
)

func (c *contextPropagatorOpt) configureHttpClient(client *HTTPClient) error {
	client.contextPropagator = c.propagator
	return nil
}

func (c *contextPropagatorOpt) configureRawClient(rawClient *RawClient) error {
	rawClient.contextPropagator = c.propagator
	return nil
}

func (c *contextPropagatorOpt) configureHttpHandler(server *httpServer) error {
	server.contextPropagator = c.propagator
	return nil
}

func (c *contextPropagatorOpt) configureRawServer(server *RawServer) error {
	server.contextPropagator = c.propagator
	return nil
}

// WithContextPropagation enables context propagation using the provided
// ContextPropagator. It can be used with NewHTTPClient, NewRawClient,
// NewHTTPHandler, and NewRawServer.
func WithContextPropagation(propagator ContextPropagator) *contextPropagatorOpt {
	return &contextPropagatorOpt{propagator: propagator}
}

func WithTrace(val bool) HttpClientOpt {
	return HttpClientOptFunc(func(c *HTTPClient) error {
		c.withTrace = val
		return nil
	})
}

func WithHttpClient(client *http.Client) HttpClientOpt {
	return HttpClientOptFunc(func(c *HTTPClient) error {
		if client == nil {
			return errors.New("http client cannot be nil")
		} else if c.client != http.DefaultClient {
			return errors.New("http client option already set")
		}

		c.client = client
		return nil
	})
}
