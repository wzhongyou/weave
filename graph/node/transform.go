package node

import (
	"context"

	"github.com/wzhongyou/weave/graph"
)

// TransformFunc is a pure function that maps one state to another.
type TransformFunc[S any] func(state S) (S, error)

// Transform wraps a pure TransformFunc as a NodeFunc (ctx is ignored).
func Transform[S any](fn TransformFunc[S]) graph.NodeFunc[S] {
	return func(_ context.Context, state S) (S, error) {
		return fn(state)
	}
}
