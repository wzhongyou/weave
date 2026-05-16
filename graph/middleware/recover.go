package middleware

import (
	"context"
	"runtime/debug"

	"github.com/wzhongyou/graphflow/graph"
)

// WithRecover wraps fn so that any panic is caught and returned as *graph.PanicError.
// Always apply this as the outermost decorator on untrusted node functions.
func WithRecover[S any](nodeName string, fn graph.NodeFunc[S]) graph.NodeFunc[S] {
	return func(ctx context.Context, state S) (out S, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = &graph.PanicError{
					NodeName: nodeName,
					Value:    r,
					Stack:    debug.Stack(),
				}
			}
		}()
		return fn(ctx, state)
	}
}
