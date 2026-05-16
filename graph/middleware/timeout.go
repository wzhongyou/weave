package middleware

import (
	"context"
	"time"

	"github.com/wzhongyou/graphflow/graph"
)

// WithTimeout wraps fn with a per-invocation deadline.
// When the deadline is exceeded the node returns context.DeadlineExceeded.
func WithTimeout[S any](fn graph.NodeFunc[S], d time.Duration) graph.NodeFunc[S] {
	return func(ctx context.Context, state S) (S, error) {
		ctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		return fn(ctx, state)
	}
}
