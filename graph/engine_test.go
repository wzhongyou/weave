package graph_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wzhongyou/weave/graph"
)

// ── helpers ──────────────────────────────────────────────────────────────────

type intState struct{ val int }

func nodeAdd(n int) graph.NodeFunc[*intState] {
	return func(_ context.Context, s *intState) (*intState, error) {
		return &intState{val: s.val + n}, nil
	}
}

func nodeMul(n int) graph.NodeFunc[*intState] {
	return func(_ context.Context, s *intState) (*intState, error) {
		return &intState{val: s.val * n}, nil
	}
}

func nodeErr(msg string) graph.NodeFunc[*intState] {
	return func(_ context.Context, s *intState) (*intState, error) {
		return s, errors.New(msg)
	}
}

func mustCompile(t *testing.T, g *graph.Graph[*intState]) {
	t.Helper()
	if err := g.Compile(); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

func run(t *testing.T, g *graph.Graph[*intState], init int, opts ...graph.Option) (*graph.ExecutionResult[*intState], error) {
	t.Helper()
	return graph.NewEngine(g).Run(context.Background(), &intState{val: init}, opts...)
}

// ── sequential ───────────────────────────────────────────────────────────────

func TestSequential(t *testing.T) {
	g := graph.NewGraph[*intState]("seq")
	g.AddNode("a", nodeAdd(1))
	g.AddNode("b", nodeMul(2))
	g.AddNode("c", nodeAdd(10))
	g.SetEntryPoint("a")
	g.AddEdge("a", "b")
	g.AddEdge("b", "c")
	mustCompile(t, g)

	res, err := run(t, g, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (0+1)*2+10 = 12
	if res.FinalState.val != 12 {
		t.Errorf("want 12, got %d", res.FinalState.val)
	}
	if res.Termination != graph.TerminationCompleted {
		t.Errorf("want TerminationCompleted, got %s", res.Termination)
	}
}

// ── conditional routing ───────────────────────────────────────────────────────

func TestConditionalRouting(t *testing.T) {
	isPos := func(_ context.Context, s *intState) bool { return s.val > 0 }

	g := graph.NewGraph[*intState]("cond")
	g.AddNode("start", nodeAdd(0)) // identity
	g.AddNode("pos", nodeAdd(100))
	g.AddNode("neg", nodeAdd(-100))
	g.SetEntryPoint("start")
	g.AddCondition("start", graph.Condition[*intState]{If: isPos, Target: "pos"})
	g.AddCondition("start", graph.Condition[*intState]{If: nil, Target: "neg"}) // fallback
	mustCompile(t, g)

	res, err := run(t, g, 5)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalState.val != 105 {
		t.Errorf("want 105, got %d", res.FinalState.val)
	}

	res, err = run(t, g, -5)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalState.val != -105 {
		t.Errorf("want -105, got %d", res.FinalState.val)
	}
}

// ── loop / back-edge ─────────────────────────────────────────────────────────

func TestLoop(t *testing.T) {
	// increment val by 1 each iteration; stop when val >= 5
	g := graph.NewGraph[*intState]("loop")
	g.AddNode("inc", nodeAdd(1))
	g.SetEntryPoint("inc")
	g.SetMaxIterations("inc", 100)
	g.SetLoopExit("inc", func(_ context.Context, s *intState) bool {
		return s.val < 5 // false = exit when val >= 5
	})
	g.AddEdge("inc", "inc") // back edge
	mustCompile(t, g)

	res, err := run(t, g, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalState.val != 5 {
		t.Errorf("want 5, got %d", res.FinalState.val)
	}
}

func TestLoopMaxIterations(t *testing.T) {
	g := graph.NewGraph[*intState]("loop-max")
	g.AddNode("inc", nodeAdd(1))
	g.SetEntryPoint("inc")
	g.SetMaxIterations("inc", 3)
	g.AddEdge("inc", "inc")
	mustCompile(t, g)

	_, err := run(t, g, 0)
	if err == nil {
		t.Fatal("expected ErrMaxIterations, got nil")
	}
	if !errors.Is(err, graph.ErrMaxIterations) {
		t.Errorf("want ErrMaxIterations, got %v", err)
	}
}

// ── terminal node ─────────────────────────────────────────────────────────────

func TestTerminalNode(t *testing.T) {
	g := graph.NewGraph[*intState]("term")
	g.AddNode("a", nodeAdd(1))
	g.AddNode("stop", nodeAdd(0))
	g.SetEntryPoint("a")
	g.AddEdge("a", "stop")
	g.MarkTerminalNode("stop", graph.TerminationCompleted)
	mustCompile(t, g)

	res, err := run(t, g, 0)
	if err != nil {
		t.Fatal(err)
	}
	// "stop" is terminal, not executed — val stays at 1
	if res.FinalState.val != 1 {
		t.Errorf("want 1, got %d", res.FinalState.val)
	}
	if res.Termination != graph.TerminationCompleted {
		t.Errorf("want TerminationCompleted, got %s", res.Termination)
	}
}

// ── node error ────────────────────────────────────────────────────────────────

func TestNodeError(t *testing.T) {
	g := graph.NewGraph[*intState]("err")
	g.AddNode("a", nodeAdd(1))
	g.AddNode("b", nodeErr("boom"))
	g.SetEntryPoint("a")
	g.AddEdge("a", "b")
	mustCompile(t, g)

	_, err := run(t, g, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	var ge *graph.GraphError
	if !errors.As(err, &ge) {
		t.Errorf("want *GraphError, got %T: %v", err, err)
	}
}

// ── context cancellation ──────────────────────────────────────────────────────

func TestContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	g := graph.NewGraph[*intState]("cancel")
	g.AddNode("block", func(ctx context.Context, s *intState) (*intState, error) {
		select {
		case <-ctx.Done():
			return s, ctx.Err()
		case <-blocked:
			return s, nil
		}
	})
	g.SetEntryPoint("block")
	mustCompile(t, g)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := graph.NewEngine(g).Run(ctx, &intState{})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

// ── global timeout ────────────────────────────────────────────────────────────

func TestGlobalTimeout(t *testing.T) {
	g := graph.NewGraph[*intState]("timeout")
	g.AddNode("slow", func(ctx context.Context, s *intState) (*intState, error) {
		select {
		case <-ctx.Done():
			return s, ctx.Err()
		case <-time.After(5 * time.Second):
			return s, nil
		}
	})
	g.SetEntryPoint("slow")
	mustCompile(t, g)

	_, err := graph.NewEngine(g).Run(
		context.Background(),
		&intState{},
		graph.WithTimeout(30*time.Millisecond),
	)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// ── parallel fan-out / fan-in ─────────────────────────────────────────────────

func TestParallelFanOut(t *testing.T) {
	// entry → [branch-a, branch-b] → join
	var execCount atomic.Int32

	makeTraced := func(fn graph.NodeFunc[*intState]) graph.NodeFunc[*intState] {
		return func(ctx context.Context, s *intState) (*intState, error) {
			execCount.Add(1)
			return fn(ctx, s)
		}
	}

	g := graph.NewGraph[*intState]("parallel")
	g.AddNode("entry", nodeAdd(0))
	g.AddNode("branch-a", makeTraced(nodeAdd(10)))
	g.AddNode("branch-b", makeTraced(nodeAdd(20)))
	g.AddNode("join", nodeAdd(0))
	g.SetEntryPoint("entry")
	g.AddEdge("entry", "branch-a")
	g.AddEdge("entry", "branch-b")
	// Both branches point to the same fan-in node.
	g.AddEdge("branch-a", "join")
	g.AddEdge("branch-b", "join")
	g.SetMergeFunc("join", func(_ context.Context, parent *intState, branches []*intState) (*intState, error) {
		sum := parent.val
		for _, b := range branches {
			sum += b.val
		}
		return &intState{val: sum}, nil
	})
	mustCompile(t, g)

	res, err := run(t, g, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// parent=0, branch-a=10, branch-b=20 → merge: 0+10+20 = 30
	if res.FinalState.val != 30 {
		t.Errorf("want 30, got %d", res.FinalState.val)
	}
	if execCount.Load() != 2 {
		t.Errorf("want 2 branch executions, got %d", execCount.Load())
	}
}

// ── hook ──────────────────────────────────────────────────────────────────────

type recordHook struct {
	graphStarts, graphEnds int
	nodeStarts, nodeEnds   []string
}

func (h *recordHook) OnGraphStart(_ context.Context, _ string, _ *intState) { h.graphStarts++ }
func (h *recordHook) OnGraphEnd(_ context.Context, _ string, _ *intState, _ error) {
	h.graphEnds++
}
func (h *recordHook) OnNodeStart(_ context.Context, name string, _ *intState) {
	h.nodeStarts = append(h.nodeStarts, name)
}
func (h *recordHook) OnNodeEnd(_ context.Context, name string, _ *intState, _ error, _ time.Duration) {
	h.nodeEnds = append(h.nodeEnds, name)
}
func (h *recordHook) OnRetry(_ context.Context, _ string, _ int, _ error) {}

func TestHook(t *testing.T) {
	g := graph.NewGraph[*intState]("hook-test")
	g.AddNode("a", nodeAdd(1))
	g.AddNode("b", nodeAdd(2))
	g.SetEntryPoint("a")
	g.AddEdge("a", "b")
	mustCompile(t, g)

	h := &recordHook{}
	res, err := run(t, g, 0, graph.WithHook(h))
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalState.val != 3 {
		t.Errorf("want 3, got %d", res.FinalState.val)
	}
	if h.graphStarts != 1 || h.graphEnds != 1 {
		t.Errorf("want 1 graph event each, got starts=%d ends=%d", h.graphStarts, h.graphEnds)
	}
	if len(h.nodeStarts) != 2 || len(h.nodeEnds) != 2 {
		t.Errorf("want 2 node events each, got starts=%d ends=%d", len(h.nodeStarts), len(h.nodeEnds))
	}
}

// ── compile errors ────────────────────────────────────────────────────────────

func TestCompileErrors(t *testing.T) {
	t.Run("empty graph", func(t *testing.T) {
		g := graph.NewGraph[*intState]("empty")
		if err := g.Compile(); !errors.Is(err, graph.ErrGraphEmpty) {
			t.Errorf("want ErrGraphEmpty, got %v", err)
		}
	})

	t.Run("no entry point", func(t *testing.T) {
		g := graph.NewGraph[*intState]("no-entry")
		g.AddNode("a", nodeAdd(1))
		if err := g.Compile(); !errors.Is(err, graph.ErrEntryNotSet) {
			t.Errorf("want ErrEntryNotSet, got %v", err)
		}
	})

	t.Run("unknown edge target", func(t *testing.T) {
		g := graph.NewGraph[*intState]("bad-edge")
		g.AddNode("a", nodeAdd(1))
		g.SetEntryPoint("a")
		g.AddEdge("a", "ghost")
		if err := g.Compile(); !errors.Is(err, graph.ErrNodeNotFound) {
			t.Errorf("want ErrNodeNotFound, got %v", err)
		}
	})

	t.Run("run without compile", func(t *testing.T) {
		g := graph.NewGraph[*intState]("uncompiled")
		g.AddNode("a", nodeAdd(1))
		g.SetEntryPoint("a")
		_, err := run(t, g, 0)
		if !errors.Is(err, graph.ErrGraphNotCompiled) {
			t.Errorf("want ErrGraphNotCompiled, got %v", err)
		}
	})
}

// ── DOT output ────────────────────────────────────────────────────────────────

func TestDOT(t *testing.T) {
	g := graph.NewGraph[*intState]("dot-test")
	g.AddNode("a", nodeAdd(1))
	g.AddNode("b", nodeAdd(2))
	g.SetEntryPoint("a")
	g.AddEdge("a", "b")
	mustCompile(t, g)

	dot := g.DOT()
	if dot == "" {
		t.Error("DOT returned empty string")
	}
	for _, want := range []string{"digraph", "dot-test", `"a"`, `"b"`} {
		if !strings.Contains(dot, want) {
			t.Errorf("DOT output missing %q", want)
		}
	}
}

// ── execution result fields ───────────────────────────────────────────────────

func TestExecutionResultFields(t *testing.T) {
	g := graph.NewGraph[*intState]("result")
	g.AddNode("a", nodeAdd(1))
	g.SetEntryPoint("a")
	mustCompile(t, g)

	res, err := run(t, g, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.GraphName != "result" {
		t.Errorf("want GraphName=result, got %q", res.GraphName)
	}
	if res.ExecutionID == "" {
		t.Error("ExecutionID should not be empty")
	}
	if res.TotalDuration <= 0 {
		t.Error("TotalDuration should be positive")
	}
	if res.TotalSteps != 1 {
		t.Errorf("want TotalSteps=1, got %d", res.TotalSteps)
	}
}

// ── per-node timeout ──────────────────────────────────────────────────────────

func TestNodeTimeout(t *testing.T) {
	g := graph.NewGraph[*intState]("node-timeout")
	g.AddNode("slow", func(ctx context.Context, s *intState) (*intState, error) {
		select {
		case <-ctx.Done():
			return s, ctx.Err()
		case <-time.After(5 * time.Second):
			return s, nil
		}
	})
	g.SetEntryPoint("slow")
	g.SetNodeTimeout("slow", 30*time.Millisecond)
	mustCompile(t, g)

	_, err := run(t, g, 0)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// ── concurrent Run calls (no data race) ──────────────────────────────────────

func TestConcurrentRuns(t *testing.T) {
	g := graph.NewGraph[*intState]("concurrent")
	g.AddNode("a", nodeAdd(1))
	g.AddNode("b", nodeMul(2))
	g.SetEntryPoint("a")
	g.AddEdge("a", "b")
	mustCompile(t, g)

	eng := graph.NewEngine(g)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			res, err := eng.Run(context.Background(), &intState{val: n})
			if err != nil {
				errs <- err
				return
			}
			if want := (n + 1) * 2; res.FinalState.val != want {
				errs <- errors.New("wrong result")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
