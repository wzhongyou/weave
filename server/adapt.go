package server

import (
	"context"
	"encoding/json"

	"github.com/wzhongyou/graphflow/graph"
)

// Adapt wraps a typed Engine[S] into a type-erased GraphHandler.
// Input is JSON-decoded into S, and the full ExecutionResult[S] is JSON-encoded as output.
//
// Example:
//
//	handler := server.Adapt(engine, graph.WithTimeout(30*time.Second))
//	srv.Register("my-graph", handler)
func Adapt[S any](engine *graph.Engine[S], opts ...graph.Option) GraphHandler {
	return AdaptWithCodec(engine,
		func(raw []byte) (S, error) {
			var s S
			if len(raw) == 0 {
				return s, nil
			}
			if err := json.Unmarshal(raw, &s); err != nil {
				return s, err
			}
			return s, nil
		},
		func(result *graph.ExecutionResult[S]) ([]byte, error) {
			return json.Marshal(result)
		},
		opts...,
	)
}

// AdaptWithCodec wraps a typed Engine[S] with custom encode/decode functions.
// decode converts raw bytes into the initial state S.
// encode converts the ExecutionResult[S] into response bytes.
func AdaptWithCodec[S any](
	engine *graph.Engine[S],
	decode func([]byte) (S, error),
	encode func(*graph.ExecutionResult[S]) ([]byte, error),
	opts ...graph.Option,
) GraphHandler {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		state, err := decode(input)
		if err != nil {
			return nil, err
		}
		result, err := engine.Run(ctx, state, opts...)
		if err != nil {
			return nil, err
		}
		return encode(result)
	}
}
