package middleware

import (
	"context"

	"github.com/wzhongyou/weave/graph"
)

// Limiter is the interface for rate-limiting strategies (token bucket, leaky bucket, etc.).
type Limiter interface {
	// Wait blocks until a token is available or ctx is cancelled.
	Wait(ctx context.Context) error
}

// WithRateLimit wraps fn so it calls limiter.Wait before each invocation.
func WithRateLimit[S any](fn graph.NodeFunc[S], limiter Limiter) graph.NodeFunc[S] {
	return func(ctx context.Context, state S) (S, error) {
		if err := limiter.Wait(ctx); err != nil {
			var zero S
			return zero, err
		}
		return fn(ctx, state)
	}
}
