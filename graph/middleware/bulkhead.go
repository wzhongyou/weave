package middleware

import (
	"context"

	"github.com/wzhongyou/graphflow/graph"
)

// Bulkhead limits the number of concurrent executions of fn.
// Excess callers receive graph.ErrBulkheadFull immediately (non-blocking).
type Bulkhead struct {
	sem chan struct{}
}

// NewBulkhead creates a Bulkhead that allows at most maxConcurrent parallel calls.
func NewBulkhead(maxConcurrent int) *Bulkhead {
	return &Bulkhead{sem: make(chan struct{}, maxConcurrent)}
}

// WithBulkhead wraps fn with bh.
func WithBulkhead[S any](fn graph.NodeFunc[S], bh *Bulkhead) graph.NodeFunc[S] {
	return func(ctx context.Context, state S) (S, error) {
		select {
		case bh.sem <- struct{}{}:
			defer func() { <-bh.sem }()
		default:
			var zero S
			return zero, graph.ErrBulkheadFull
		}
		return fn(ctx, state)
	}
}
