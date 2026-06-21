package middleware

import (
	"context"
	"math/rand"
	"time"

	"github.com/wzhongyou/weave/graph"
)

// RetryPolicy configures retry behaviour.
type RetryPolicy struct {
	MaxAttempts int           // total attempts including the first (default 3)
	Backoff     time.Duration // initial backoff (default 500ms)
	MaxBackoff  time.Duration // cap on backoff (default 30s)
	Multiplier  float64       // backoff growth factor (default 2.0)
	Jitter      bool          // add ±25% random jitter
	// RetryOn restricts retries to specific errors; nil = retry on any error.
	RetryOn func(err error) bool
}

// WithRetry wraps fn so that transient failures are retried according to p.
// Panics are NOT retried; use WithRecover before WithRetry to convert them.
func WithRetry[S any](fn graph.NodeFunc[S], p RetryPolicy) graph.NodeFunc[S] {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 3
	}
	if p.Backoff <= 0 {
		p.Backoff = 500 * time.Millisecond
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = 30 * time.Second
	}
	if p.Multiplier <= 1 {
		p.Multiplier = 2.0
	}

	return func(ctx context.Context, state S) (S, error) {
		backoff := p.Backoff
		var lastErr error

		for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
			s, err := fn(ctx, state)
			if err == nil {
				return s, nil
			}
			lastErr = err

			// Stop immediately if this error type should not be retried.
			if p.RetryOn != nil && !p.RetryOn(err) {
				return s, err
			}

			if attempt == p.MaxAttempts {
				break
			}

			sleep := backoff
			if p.Jitter {
				// ±25 % random jitter
				delta := float64(backoff) * 0.25
				sleep += time.Duration((2*rand.Float64() - 1) * delta)
			}
			if sleep < 0 {
				sleep = 0
			}

			select {
			case <-ctx.Done():
				return s, ctx.Err()
			case <-time.After(sleep):
			}

			backoff = time.Duration(float64(backoff) * p.Multiplier)
			if backoff > p.MaxBackoff {
				backoff = p.MaxBackoff
			}
		}

		var zero S
		return zero, lastErr
	}
}
