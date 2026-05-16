package graph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/wzhongyou/graphflow/graph/checkpoint"
)

// CheckpointManager is the persistence interface used by Engine.Run.
// checkpoint.InMemoryManager and checkpoint.FileManager both satisfy this interface.
type CheckpointManager = checkpoint.Manager

// Engine executes a compiled Graph.
type Engine[S any] struct {
	graph *Graph[S]
}

// NewEngine creates an engine for the given compiled graph.
func NewEngine[S any](g *Graph[S]) *Engine[S] { return &Engine[S]{graph: g} }

// Run executes the graph from its entry point and returns the execution result.
// The graph must have been compiled with Compile before calling Run.
func (e *Engine[S]) Run(ctx context.Context, initialState S, opts ...Option) (*ExecutionResult[S], error) {
	if !e.graph.compiled {
		return nil, ErrGraphNotCompiled
	}

	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.globalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.globalTimeout)
		defer cancel()
	}

	hook := hookOf[S](cfg.hook)
	state := initialState
	iterCount := make(map[string]int)
	start := time.Now()
	execID := newID()
	steps := 0

	if hook != nil {
		hook.OnGraphStart(ctx, e.graph.name, state)
	}

	mkResult := func(term TerminationReason, err error) *ExecutionResult[S] {
		return &ExecutionResult[S]{
			FinalState:    state,
			GraphName:     e.graph.name,
			ExecutionID:   execID,
			StartTime:     start,
			EndTime:       time.Now(),
			Termination:   term,
			Error:         err,
			TotalNodes:    len(e.graph.nodes),
			NodeCount:     steps,
			TotalSteps:    steps,
			TotalDuration: time.Since(start),
		}
	}

	finish := func(term TerminationReason, err error) (*ExecutionResult[S], error) {
		if hook != nil {
			hook.OnGraphEnd(ctx, e.graph.name, state, err)
		}
		return mkResult(term, err), err
	}

	for current := e.graph.entry; current != ""; {
		if err := ctx.Err(); err != nil {
			return finish(TerminationCancelled, err)
		}

		if reason, ok := e.graph.terminal[current]; ok {
			return finish(reason, nil)
		}

		var err error
		state, err = e.execNode(ctx, cfg, hook, current, state)
		if err != nil {
			graphErr := &GraphError{GraphName: e.graph.name, ExecutionID: execID, Cause: err}
			return finish(TerminationError, graphErr)
		}
		steps++

		targets, err := e.resolveTargets(ctx, current, state, iterCount)
		if err != nil {
			graphErr := &GraphError{GraphName: e.graph.name, ExecutionID: execID, Cause: err}
			return finish(TerminationError, graphErr)
		}

		switch len(targets) {
		case 0:
			current = ""
		case 1:
			current = targets[0]
		default:
			// Parallel fan-out: run all targets concurrently, merge at fan-in.
			fanIn, merged, pErr := e.runParallel(ctx, cfg, hook, targets, state, iterCount)
			if pErr != nil {
				graphErr := &GraphError{GraphName: e.graph.name, ExecutionID: execID, Cause: pErr}
				return finish(TerminationError, graphErr)
			}
			state = merged
			current = fanIn
		}
	}

	return finish(TerminationCompleted, nil)
}

// execNode runs a single node, honouring per-node timeouts and emitting hook events.
func (e *Engine[S]) execNode(ctx context.Context, _ *runConfig, hook Hook[S], name string, state S) (S, error) {
	node := e.graph.nodes[name]

	if hook != nil {
		hook.OnNodeStart(ctx, name, state)
	}

	nodeCtx := ctx
	var cancel context.CancelFunc
	if node.timeout > 0 {
		nodeCtx, cancel = context.WithTimeout(ctx, node.timeout)
		defer cancel()
	}

	t0 := time.Now()
	out, err := node.fn(nodeCtx, state)
	dur := time.Since(t0)

	if hook != nil {
		hook.OnNodeEnd(ctx, name, out, err, dur)
	}

	if err != nil {
		return state, &NodeError{NodeName: name, Attempt: 1, Cause: err}
	}
	return out, nil
}

// resolveTargets determines the next nodes after from.
// Returns nil for normal completion, one element for sequential, many for fan-out.
func (e *Engine[S]) resolveTargets(ctx context.Context, from string, state S, iterCount map[string]int) ([]string, error) {
	edges := e.graph.edges[from]
	if len(edges) == 0 {
		return nil, nil
	}

	// Conditional edges take priority, evaluated in definition order.
	var unconditional []*internalEdge[S]
	for _, edge := range edges {
		if edge.condition == nil {
			unconditional = append(unconditional, edge)
			continue
		}
		if edge.condition(ctx, state) {
			next, err := e.followEdge(ctx, edge, state, iterCount)
			if err != nil {
				return nil, err
			}
			if next == "" {
				return nil, nil
			}
			return []string{next}, nil
		}
	}

	// No condition matched — take all unconditional edges (fan-out when > 1).
	if len(unconditional) == 0 {
		return nil, nil
	}
	targets := make([]string, 0, len(unconditional))
	for _, edge := range unconditional {
		next, err := e.followEdge(ctx, edge, state, iterCount)
		if err != nil {
			return nil, err
		}
		if next != "" {
			targets = append(targets, next)
		}
	}
	return targets, nil
}

// followEdge transitions to edge.to, enforcing loop limits for back edges.
func (e *Engine[S]) followEdge(ctx context.Context, edge *internalEdge[S], state S, iterCount map[string]int) (string, error) {
	if !edge.isBack {
		return edge.to, nil
	}

	iterCount[edge.to]++
	lc := e.graph.loops[edge.to]
	if lc == nil {
		lc = &loopConfig[S]{maxIterations: defaultMaxIterations}
	}

	if iterCount[edge.to] > lc.maxIterations {
		return "", fmt.Errorf("%w: node %q exceeded %d iterations",
			ErrMaxIterations, edge.to, lc.maxIterations)
	}
	if lc.exitCond != nil && !lc.exitCond(ctx, state) {
		return "", nil
	}
	return edge.to, nil
}

// ── Options ───────────────────────────────────────────────────────────────────

// Option configures a single Run call.
type Option func(*runConfig)

// runConfig holds all per-run settings.
type runConfig struct {
	hook           any // Hook[S] stored as any; type-asserted in hookOf
	checkpointMgr  CheckpointManager
	checkpointFreq int
	resumeFrom     string
	maxConcurrency int
	globalTimeout  time.Duration
	panicRecovery  bool
}

// WithHook attaches a lifecycle hook to this Run.
func WithHook[S any](h Hook[S]) Option { return func(c *runConfig) { c.hook = h } }

// WithCheckpoint enables checkpoint persistence via mgr.
func WithCheckpoint(mgr CheckpointManager) Option {
	return func(c *runConfig) { c.checkpointMgr = mgr }
}

// WithCheckpointFreq sets how often (every N nodes) the engine saves a checkpoint.
func WithCheckpointFreq(n int) Option { return func(c *runConfig) { c.checkpointFreq = n } }

// WithResumeFrom resumes execution from checkpointID.
// Pass "" to use the latest checkpoint for this graph.
func WithResumeFrom(checkpointID string) Option {
	return func(c *runConfig) { c.resumeFrom = checkpointID }
}

// WithMaxConcurrency caps the goroutines used for parallel branches.
func WithMaxConcurrency(n int) Option { return func(c *runConfig) { c.maxConcurrency = n } }

// WithTimeout sets a global deadline for the entire graph execution.
func WithTimeout(d time.Duration) Option { return func(c *runConfig) { c.globalTimeout = d } }

// WithPanicRecovery auto-wraps every node with panic recovery.
func WithPanicRecovery(enabled bool) Option {
	return func(c *runConfig) { c.panicRecovery = enabled }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// hookOf extracts a Hook[S] from an any-boxed value; returns nil on type mismatch.
func hookOf[S any](h any) Hook[S] {
	if h == nil {
		return nil
	}
	typed, _ := h.(Hook[S])
	return typed
}

// newID generates a random 8-byte hex ID for execution tracking.
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
