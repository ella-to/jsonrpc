package jsonrpc_test

import (
	"context"
	"testing"

	"ella.to/jsonrpc"
)

func BenchmarkPeerRoundtrip(b *testing.B) {
	echo := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		return req.CreateResponse("ok")
	})
	peer1, peer2 := jsonrpc.NewRawPeerPair(echo, echo)
	ctx := context.Background()
	go peer1.Serve(ctx)
	go peer2.Serve(ctx)
	defer peer1.Close()
	defer peer2.Close()

	for b.Loop() {
		if _, err := peer1.Call(ctx, jsonrpc.WithRequest("echo", nil, false)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPeerBatchRoundtrip(b *testing.B) {
	echo := jsonrpc.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		return req.CreateResponse("ok")
	})
	peer1, peer2 := jsonrpc.NewRawPeerPair(echo, echo)
	ctx := context.Background()
	go peer1.Serve(ctx)
	go peer2.Serve(ctx)
	defer peer1.Close()
	defer peer2.Close()

	for b.Loop() {
		reqs := make([]*jsonrpc.Request, 10)
		for j := range reqs {
			reqs[j] = jsonrpc.WithRequest("echo", nil, false)
		}
		if _, err := peer1.Call(ctx, reqs...); err != nil {
			b.Fatal(err)
		}
	}
}
