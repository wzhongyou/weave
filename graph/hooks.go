package graph

import (
	"context"
	"time"
)

// Hook receives lifecycle events from the engine.
type Hook[S any] interface {
	OnGraphStart(ctx context.Context, graphName string, state S)
	OnGraphEnd(ctx context.Context, graphName string, state S, err error)
	OnNodeStart(ctx context.Context, nodeName string, state S)
	OnNodeEnd(ctx context.Context, nodeName string, state S, err error, duration time.Duration)
	OnRetry(ctx context.Context, nodeName string, attempt int, lastErr error)
}

// composeHook fans out every call to each hook in order.
type composeHook[S any] struct{ hooks []Hook[S] }

func (c *composeHook[S]) OnGraphStart(ctx context.Context, name string, state S) {
	for _, h := range c.hooks {
		h.OnGraphStart(ctx, name, state)
	}
}
func (c *composeHook[S]) OnGraphEnd(ctx context.Context, name string, state S, err error) {
	for _, h := range c.hooks {
		h.OnGraphEnd(ctx, name, state, err)
	}
}
func (c *composeHook[S]) OnNodeStart(ctx context.Context, name string, state S) {
	for _, h := range c.hooks {
		h.OnNodeStart(ctx, name, state)
	}
}
func (c *composeHook[S]) OnNodeEnd(ctx context.Context, name string, state S, err error, d time.Duration) {
	for _, h := range c.hooks {
		h.OnNodeEnd(ctx, name, state, err, d)
	}
}
func (c *composeHook[S]) OnRetry(ctx context.Context, name string, attempt int, lastErr error) {
	for _, h := range c.hooks {
		h.OnRetry(ctx, name, attempt, lastErr)
	}
}

// ComposeHooks combines multiple hooks into one; calls are forwarded in order.
func ComposeHooks[S any](hooks ...Hook[S]) Hook[S] {
	switch len(hooks) {
	case 0:
		return nil
	case 1:
		return hooks[0]
	default:
		return &composeHook[S]{hooks: hooks}
	}
}
