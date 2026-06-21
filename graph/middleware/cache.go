package middleware

import (
	"context"

	"github.com/wzhongyou/weave/graph"
)

// Cache stores and retrieves node results keyed by an idempotency key.
type Cache[S any] interface {
	Get(ctx context.Context, key string) (S, bool, error)
	Set(ctx context.Context, key string, state S) error
}

// KeyFunc derives a cache key from the current state.
// Return "" to skip caching for this invocation.
type KeyFunc[S any] func(ctx context.Context, state S) string

// WithCache wraps fn so that results for the same key are served from cache.
func WithCache[S any](fn graph.NodeFunc[S], cache Cache[S], keyFn KeyFunc[S]) graph.NodeFunc[S] {
	return func(ctx context.Context, state S) (S, error) {
		key := keyFn(ctx, state)
		if key != "" {
			if cached, ok, err := cache.Get(ctx, key); err == nil && ok {
				return cached, nil
			}
		}

		result, err := fn(ctx, state)
		if err != nil {
			return result, err
		}

		if key != "" {
			_ = cache.Set(ctx, key, result) // cache miss on Set is non-fatal
		}
		return result, nil
	}
}
