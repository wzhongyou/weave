package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// runParallel executes all target nodes concurrently, each with an isolated copy
// of parentState. When all branches complete it determines the fan-in node
// (all branches must converge on a single common successor), applies the
// registered MergeFunc if present, and returns the merged state and fan-in name.
func (e *Engine[S]) runParallel(
	ctx context.Context,
	cfg *runConfig,
	hook Hook[S],
	targets []string,
	parentState S,
	iterCount map[string]int,
) (fanIn string, merged S, err error) {
	type branchResult struct {
		state     S
		nextNodes []string
		err       error
	}

	results := make([]branchResult, len(targets))

	var sem chan struct{}
	if cfg.maxConcurrency > 0 {
		sem = make(chan struct{}, cfg.maxConcurrency)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()

			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-cancelCtx.Done():
					results[i].err = cancelCtx.Err()
					return
				}
			}

			branchState, cpErr := deepCopyState[S](parentState)
			if cpErr != nil {
				results[i].err = fmt.Errorf("branch %q: %w", target, cpErr)
				cancel()
				return
			}

			branchState, execErr := e.execNode(cancelCtx, cfg, hook, target, branchState)
			if execErr != nil {
				results[i].err = execErr
				cancel()
				return
			}

			// Each branch uses its own iterCount copy so back-edge counters
			// don't bleed across branches.
			localIter := make(map[string]int, len(iterCount))
			for k, v := range iterCount {
				localIter[k] = v
			}

			nextNodes, resolveErr := e.resolveTargets(cancelCtx, target, branchState, localIter)
			results[i] = branchResult{state: branchState, nextNodes: nextNodes, err: resolveErr}
		}(i, target)
	}
	wg.Wait()

	// Aggregate branch errors.
	var me MultiError
	for _, r := range results {
		me.Append(r.err)
	}
	if mergeErr := me.ToError(); mergeErr != nil {
		var zero S
		return "", zero, mergeErr
	}

	// All branches must converge on the same single next node (the fan-in).
	fanInNode := ""
	for idx, r := range results {
		switch len(r.nextNodes) {
		case 0:
			// Branch terminates; fanInNode stays "".
		case 1:
			if fanInNode == "" {
				fanInNode = r.nextNodes[0]
			} else if fanInNode != r.nextNodes[0] {
				var zero S
				return "", zero, fmt.Errorf(
					"parallel branches disagree on fan-in: %q (branch 0) vs %q (branch %d)",
					fanInNode, r.nextNodes[0], idx,
				)
			}
		default:
			var zero S
			return "", zero, fmt.Errorf(
				"branch %q produced a nested fan-out (%d targets); nested parallelism is not supported",
				targets[idx], len(r.nextNodes),
			)
		}
	}

	branchStates := make([]S, len(results))
	for i, r := range results {
		branchStates[i] = r.state
	}

	// Apply the merge function registered for the fan-in node, if any.
	if fanInNode != "" {
		if mergeFn, ok := e.graph.merges[fanInNode]; ok {
			mergedState, mergeErr := mergeFn(ctx, parentState, branchStates)
			if mergeErr != nil {
				var zero S
				return "", zero, fmt.Errorf("merge at %q: %w", fanInNode, mergeErr)
			}
			return fanInNode, mergedState, nil
		}
	}

	// No merge function: use the last branch's state as the continuation state.
	if len(results) > 0 {
		return fanInNode, results[len(results)-1].state, nil
	}
	return fanInNode, parentState, nil
}

// deepCopyState returns a deep copy of state.
// It first checks whether S implements StateDeepCopier[S]; if not it falls
// back to a JSON round-trip.
func deepCopyState[S any](state S) (S, error) {
	if copier, ok := any(state).(StateDeepCopier[S]); ok {
		return copier.DeepCopy(), nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		var zero S
		return zero, fmt.Errorf("deep copy marshal: %w", err)
	}
	var cp S
	if err := json.Unmarshal(data, &cp); err != nil {
		var zero S
		return zero, fmt.Errorf("deep copy unmarshal: %w", err)
	}
	return cp, nil
}
