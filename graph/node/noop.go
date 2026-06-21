// Package node provides reusable built-in node implementations for the graph engine.
package node

import (
	"context"

	"github.com/wzhongyou/weave/graph"
)

// Noop returns the state unchanged. Useful as a placeholder or synchronisation point.
func Noop[S any](_ context.Context, state S) (S, error) { return state, nil }

// NoopFunc returns a named NodeFunc that does nothing.
// Identical to Noop but satisfies graph.NodeFunc[S] without a type assertion.
func NoopFunc[S any]() graph.NodeFunc[S] { return Noop[S] }
