package middleware

import (
	"context"
	"fmt"

	"github.com/wzhongyou/graphflow/graph"
)

// Validator checks a state value; returns a descriptive error if invalid.
type Validator[S any] func(ctx context.Context, state S) error

// WithValidate wraps fn with pre- and/or post-invocation validation.
// Pass nil for pre or post to skip that phase.
func WithValidate[S any](fn graph.NodeFunc[S], pre, post Validator[S]) graph.NodeFunc[S] {
	return func(ctx context.Context, state S) (S, error) {
		if pre != nil {
			if err := pre(ctx, state); err != nil {
				var zero S
				return zero, fmt.Errorf("%w: pre-validate: %v", graph.ErrValidation, err)
			}
		}
		out, err := fn(ctx, state)
		if err != nil {
			return out, err
		}
		if post != nil {
			if err := post(ctx, out); err != nil {
				var zero S
				return zero, fmt.Errorf("%w: post-validate: %v", graph.ErrValidation, err)
			}
		}
		return out, nil
	}
}
