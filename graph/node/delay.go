package node

import (
	"context"
	"time"

	"github.com/wzhongyou/graphflow/graph"
)

// DelayConfig configures a Delay node.
type DelayConfig struct {
	Duration time.Duration
}

// Delay pauses execution for cfg.Duration, then returns state unchanged.
// Respects context cancellation.
func Delay[S any](cfg DelayConfig) graph.NodeFunc[S] {
	return func(ctx context.Context, state S) (S, error) {
		select {
		case <-time.After(cfg.Duration):
			return state, nil
		case <-ctx.Done():
			return state, ctx.Err()
		}
	}
}
