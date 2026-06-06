// Package graph is the core graph execution engine.
package graph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ── Public types ─────────────────────────────────────────────────────────────

// NodeFunc is the signature every node must implement.
type NodeFunc[S any] func(ctx context.Context, state S) (S, error)

// Condition is one arm of a conditional edge.
// If If is nil the edge acts as the fallback (default) path.
type Condition[S any] struct {
	If     func(ctx context.Context, state S) bool
	Target string
}

// MergeFunc merges parallel branch states into one for fan-in nodes.
type MergeFunc[S any] func(ctx context.Context, parent S, branches []S) (S, error)

// StateSerializer is optionally implemented by S to customise checkpoint encoding.
// Default implementation uses gob.
type StateSerializer interface {
	MarshalState() ([]byte, error)
	UnmarshalState([]byte) error
}

// StateDeepCopier is optionally implemented by S for parallel branch isolation.
// Default implementation uses a gob round-trip.
type StateDeepCopier[S any] interface {
	DeepCopy() S
}

// ── Internal types ────────────────────────────────────────────────────────────

type internalNode[S any] struct {
	fn      NodeFunc[S]
	timeout time.Duration // 0 = no per-node timeout
}

type internalEdge[S any] struct {
	to        string
	condition func(ctx context.Context, state S) bool // nil = unconditional
	label     string                                  // for DOT output
	isBack    bool                                    // set by Compile via DFS
}

type loopConfig[S any] struct {
	maxIterations int
	exitCond      func(ctx context.Context, state S) bool // false = exit loop
}

const defaultMaxIterations = 1000

// ── Graph ─────────────────────────────────────────────────────────────────────

// Graph is a type-safe, compiled execution graph.
// Build it with the Add*/Set* methods, then call Compile before passing to NewEngine.
type Graph[S any] struct {
	name     string
	nodes    map[string]*internalNode[S]
	edges    map[string][]*internalEdge[S] // from → outgoing edges
	entry    string
	merges   map[string]MergeFunc[S]
	loops    map[string]*loopConfig[S]
	terminal map[string]TerminationReason
	compiled bool
}

// NewGraph creates an empty, uncompiled graph.
func NewGraph[S any](name string) *Graph[S] {
	return &Graph[S]{
		name:     name,
		nodes:    make(map[string]*internalNode[S]),
		edges:    make(map[string][]*internalEdge[S]),
		merges:   make(map[string]MergeFunc[S]),
		loops:    make(map[string]*loopConfig[S]),
		terminal: make(map[string]TerminationReason),
	}
}

// AddNode registers fn under name. Must be called before Compile.
func (g *Graph[S]) AddNode(name string, fn NodeFunc[S]) {
	g.nodes[name] = &internalNode[S]{fn: fn}
}

// SetNodeTimeout sets a per-invocation deadline for name.
func (g *Graph[S]) SetNodeTimeout(name string, d time.Duration) {
	if n, ok := g.nodes[name]; ok {
		n.timeout = d
	}
}

// SetEntryPoint designates the node where execution begins.
func (g *Graph[S]) SetEntryPoint(name string) { g.entry = name }

// AddEdge adds an unconditional edge from → to.
func (g *Graph[S]) AddEdge(from, to string) {
	g.edges[from] = append(g.edges[from], &internalEdge[S]{to: to})
}

// AddCondition adds a conditional edge from from. Conditions are evaluated in
// the order they were added; first match wins. A nil If acts as the fallback.
func (g *Graph[S]) AddCondition(from string, c Condition[S]) {
	g.edges[from] = append(g.edges[from], &internalEdge[S]{
		to:        c.Target,
		condition: c.If,
	})
}

// SetMergeFunc sets the merge function called when multiple branches converge on node.
func (g *Graph[S]) SetMergeFunc(node string, fn MergeFunc[S]) { g.merges[node] = fn }

// SetMaxIterations caps the number of times node may be re-entered via a back edge.
// Default: 1000.
func (g *Graph[S]) SetMaxIterations(node string, n int) { g.ensureLoop(node).maxIterations = n }

// SetLoopExit sets the exit predicate for a loop node.
// The predicate receives state just before re-entry; returning false exits the loop.
func (g *Graph[S]) SetLoopExit(node string, fn func(ctx context.Context, state S) bool) {
	g.ensureLoop(node).exitCond = fn
}

// MarkTerminalNode marks node as a terminal node that stops execution with reason.
func (g *Graph[S]) MarkTerminalNode(node string, reason TerminationReason) {
	g.terminal[node] = reason
}

// Compile validates the graph structure and detects back edges.
// Must be called once before passing the graph to NewEngine.
func (g *Graph[S]) Compile() error {
	if len(g.nodes) == 0 {
		return ErrGraphEmpty
	}
	if g.entry == "" {
		return ErrEntryNotSet
	}
	if _, ok := g.nodes[g.entry]; !ok {
		return fmt.Errorf("%w: entry %q", ErrNodeNotFound, g.entry)
	}
	for from, edges := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return fmt.Errorf("%w: edge source %q", ErrNodeNotFound, from)
		}
		for _, e := range edges {
			if _, ok := g.nodes[e.to]; !ok {
				return fmt.Errorf("%w: edge target %q (from %q)", ErrNodeNotFound, e.to, from)
			}
		}
	}

	g.markBackEdges()

	// Ensure all loop-entry nodes have a max-iterations cap.
	for _, edges := range g.edges {
		for _, e := range edges {
			if e.isBack {
				lc := g.ensureLoop(e.to)
				if lc.maxIterations == 0 {
					lc.maxIterations = defaultMaxIterations
				}
			}
		}
	}

	g.compiled = true
	return nil
}

// DOT returns a Graphviz DOT string useful for debugging and visualisation.
func (g *Graph[S]) DOT() string {
	var b strings.Builder
	fmt.Fprintf(&b, "digraph %q {\n  rankdir=LR;\n", g.name)

	for name := range g.nodes {
		shape := "box"
		if name == g.entry {
			shape = "doublecircle"
		} else if _, ok := g.terminal[name]; ok {
			shape = "circle"
		}
		fmt.Fprintf(&b, "  %q [shape=%s];\n", name, shape)
	}

	for from, edges := range g.edges {
		for _, e := range edges {
			var attrs []string
			if e.isBack {
				attrs = append(attrs, "style=dashed")
			}
			label := e.label
			if e.condition != nil && label == "" {
				label = "cond"
			}
			if label != "" {
				attrs = append(attrs, fmt.Sprintf("label=%q", label))
			}
			attrStr := ""
			if len(attrs) > 0 {
				attrStr = " [" + strings.Join(attrs, ", ") + "]"
			}
			fmt.Fprintf(&b, "  %q -> %q%s;\n", from, e.to, attrStr)
		}
	}

	fmt.Fprintf(&b, "}\n")
	return b.String()
}

// LoadFromFile loads a graph from a YAML config file and the provided registry.
func LoadFromFile[S any](path string, reg *Registry[S]) (*Graph[S], error) {
	cfg, err := loadConfigFile(path, nil)
	if err != nil {
		return nil, err
	}

	g := NewGraph[S](cfg.Name)

	// Register nodes.
	for _, ndef := range cfg.Nodes {
		fn, ok := reg.lookupNode(ndef.Type)
		if !ok {
			return nil, fmt.Errorf("LoadFromFile: node %q references unknown type %q", ndef.Name, ndef.Type)
		}
		g.AddNode(ndef.Name, fn)
		if ndef.Timeout != "" {
			d, err := time.ParseDuration(ndef.Timeout)
			if err != nil {
				return nil, fmt.Errorf("LoadFromFile: node %q: invalid timeout %q: %w", ndef.Name, ndef.Timeout, err)
			}
			g.SetNodeTimeout(ndef.Name, d)
		}
	}

	// Register edges.
	for _, edef := range cfg.Edges {
		if err := applyEdgeDef(g, reg, edef); err != nil {
			return nil, err
		}
	}

	// Register loops.
	for _, ldef := range cfg.Loops {
		if ldef.MaxIterations > 0 {
			g.SetMaxIterations(ldef.Node, ldef.MaxIterations)
		}
		if ldef.ExitCondition != "" {
			exitFn, ok := reg.lookupExit(ldef.ExitCondition)
			if !ok {
				return nil, fmt.Errorf("LoadFromFile: loop on %q references unknown exit condition %q", ldef.Node, ldef.ExitCondition)
			}
			g.SetLoopExit(ldef.Node, exitFn)
		}
	}

	g.SetEntryPoint(cfg.Entry)

	if err := g.Compile(); err != nil {
		return nil, fmt.Errorf("LoadFromFile: compile: %w", err)
	}

	return g, nil
}

// ── Config loading helpers ────────────────────────────────────────────────────

// loadConfigFile reads a YAML config file, resolves includes, and returns the merged config.
func loadConfigFile(path string, seen map[string]bool) (*Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("loadConfig: %w", err)
	}

	if seen == nil {
		seen = make(map[string]bool)
	}
	if seen[absPath] {
		return nil, fmt.Errorf("loadConfig: circular include detected: %s", path)
	}
	seen[absPath] = true

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("loadConfig: read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("loadConfig: parse %s: %w", path, err)
	}

	baseDir := filepath.Dir(absPath)
	for _, inc := range cfg.Include {
		incPath := filepath.Join(baseDir, inc)
		subCfg, err := loadConfigFile(incPath, seen)
		if err != nil {
			return nil, err
		}
		cfg.Nodes = append(cfg.Nodes, subCfg.Nodes...)
		cfg.Edges = append(cfg.Edges, subCfg.Edges...)
		cfg.Loops = append(cfg.Loops, subCfg.Loops...)
	}

	return &cfg, nil
}

// applyEdgeDef adds edges from a single EdgeDef to the graph.
func applyEdgeDef[S any](g *Graph[S], reg *Registry[S], edef EdgeDef) error {
	fromList := edef.From.Slice()
	toList := edef.To.Slice()

	switch {
	case edef.Condition != "" && len(edef.Branches) > 0:
		// Conditional edge: one source, condition function evaluates to branch name.
		if len(fromList) != 1 {
			return fmt.Errorf("LoadFromFile: conditional edge with multi-from is not supported (from=%v)", fromList)
		}
		condFn, ok := reg.lookupCondition(edef.Condition)
		if !ok {
			return fmt.Errorf("LoadFromFile: condition %q not registered", edef.Condition)
		}
		from := fromList[0]
		for _, branch := range edef.Branches {
			if branch.When == "default" {
				g.AddCondition(from, Condition[S]{If: nil, Target: branch.To})
			} else {
				when := branch.When
				target := branch.To
				g.AddCondition(from, Condition[S]{
					If:     func(ctx context.Context, state S) bool { return condFn(ctx, state) == when },
					Target: target,
				})
			}
		}

	case edef.Condition != "" && len(edef.Branches) == 0:
		return fmt.Errorf("LoadFromFile: condition %q set but no branches defined", edef.Condition)

	case len(fromList) > 1:
		// Fan-in: multiple sources → single target.
		if len(toList) != 1 {
			return fmt.Errorf("LoadFromFile: fan-in edge requires exactly one 'to' (got %v)", toList)
		}
		to := toList[0]
		if edef.Merge != "" {
			mergeFn, ok := reg.lookupMerge(edef.Merge)
			if !ok {
				return fmt.Errorf("LoadFromFile: merge %q not registered", edef.Merge)
			}
			g.SetMergeFunc(to, mergeFn)
		}
		for _, from := range fromList {
			g.AddEdge(from, to)
		}

	case len(toList) > 1:
		// Fan-out: single source → multiple targets (parallel).
		if len(fromList) != 1 {
			return fmt.Errorf("LoadFromFile: fan-out with multi-from is not supported (from=%v, to=%v)", fromList, toList)
		}
		for _, to := range toList {
			g.AddEdge(fromList[0], to)
		}

	default:
		// Simple edge: single from → single to.
		if len(fromList) == 1 && len(toList) == 1 {
			g.AddEdge(fromList[0], toList[0])
		} else {
			return fmt.Errorf("LoadFromFile: invalid edge (from=%v, to=%v)", fromList, toList)
		}
	}

	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (g *Graph[S]) ensureLoop(node string) *loopConfig[S] {
	if g.loops[node] == nil {
		g.loops[node] = &loopConfig[S]{}
	}
	return g.loops[node]
}

// markBackEdges runs DFS from the entry node and tags back edges.
// Colour: 0=unvisited, 1=in-stack, 2=done.
func (g *Graph[S]) markBackEdges() {
	colour := make(map[string]int8)
	var dfs func(string)
	dfs = func(n string) {
		colour[n] = 1
		for _, e := range g.edges[n] {
			switch colour[e.to] {
			case 0:
				dfs(e.to)
			case 1: // ancestor still on stack → back edge
				e.isBack = true
			}
		}
		colour[n] = 2
	}
	dfs(g.entry)
}
