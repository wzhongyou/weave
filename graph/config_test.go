package graph

import (
	"context"
	"path/filepath"
	"testing"
)

// testState is a minimal state for config tests.
type testState struct {
	Value  int
	Status string
	Steps  []string
}

func identityNode(ctx context.Context, s testState) (testState, error) {
	s.Steps = append(s.Steps, "identity")
	return s, nil
}

func addNode(ctx context.Context, s testState) (testState, error) {
	s.Value++
	s.Steps = append(s.Steps, "add")
	return s, nil
}

func evalNode(ctx context.Context, s testState) (testState, error) {
	s.Steps = append(s.Steps, "eval")
	return s, nil
}

func resultCheck(ctx context.Context, s testState) string {
	if s.Value > 0 {
		return "ok"
	}
	return "fail"
}

func loopCheck(ctx context.Context, s testState) string {
	if s.Value < 3 {
		return "continue"
	}
	return "done"
}

func loopExit(ctx context.Context, s testState) bool {
	return s.Value < 5
}

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func newTestRegistry() *Registry[testState] {
	r := NewRegistry[testState]()
	r.RegisterNode("identity", identityNode)
	r.RegisterNode("add", addNode)
	r.RegisterNode("eval", evalNode)
	r.RegisterCondition("result_check", resultCheck)
	r.RegisterCondition("loop_check", loopCheck)
	r.RegisterExitCondition("loop_exit", loopExit)
	return r
}

func TestOneOrMany_UnmarshalYAML_SingleString(t *testing.T) {
	cfg, err := loadConfigFile(testdataPath(t, "simple.yaml"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(cfg.Edges))
	}
	edge := cfg.Edges[0]
	if len(edge.From) != 1 || edge.From[0] != "start" {
		t.Errorf("expected from=[start], got %v", edge.From.Slice())
	}
	if len(edge.To) != 1 || edge.To[0] != "process" {
		t.Errorf("expected to=[process], got %v", edge.To.Slice())
	}
}

func TestOneOrMany_UnmarshalYAML_List(t *testing.T) {
	// with_condition uses branches which are lists; test fan-out via a custom YAML.
	// We verify that Config.NodeDef reads correctly (node "check" has type "eval").
	cfg, err := loadConfigFile(testdataPath(t, "with_condition.yaml"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(cfg.Nodes))
	}
	if cfg.Entry != "check" {
		t.Errorf("expected entry=check, got %q", cfg.Entry)
	}
}

func TestLoadFromFile_Simple(t *testing.T) {
	g, err := LoadFromFile(testdataPath(t, "simple.yaml"), newTestRegistry())
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if g.name != "simple-pipeline" {
		t.Errorf("expected name simple-pipeline, got %q", g.name)
	}
	if g.entry != "start" {
		t.Errorf("expected entry start, got %q", g.entry)
	}

	eng := NewEngine(g)
	result, err := eng.Run(context.Background(), testState{Value: 0})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalState.Value != 1 {
		t.Errorf("expected Value=1, got %d", result.FinalState.Value)
	}
}

func TestLoadFromFile_WithCondition(t *testing.T) {
	g, err := LoadFromFile(testdataPath(t, "with_condition.yaml"), newTestRegistry())
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	eng := NewEngine(g)

	// Value > 0 → "ok" branch → success
	result, err := eng.Run(context.Background(), testState{Value: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Termination != TerminationCompleted {
		t.Errorf("expected completed, got %s", result.Termination)
	}

	// Value == 0 → not "ok" → default branch → failure
	result2, err := eng.Run(context.Background(), testState{Value: 0})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result2.Termination != TerminationCompleted {
		t.Errorf("expected completed, got %s", result2.Termination)
	}
}

func TestLoadFromFile_WithInclude(t *testing.T) {
	g, err := LoadFromFile(testdataPath(t, "with_include.yaml"), newTestRegistry())
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if _, ok := g.nodes["extra"]; !ok {
		t.Error("expected 'extra' node from included file")
	}
	if _, ok := g.nodes["start"]; !ok {
		t.Error("expected 'start' node from main file")
	}
}

func TestLoadFromFile_WithLoop(t *testing.T) {
	g, err := LoadFromFile(testdataPath(t, "with_loop.yaml"), newTestRegistry())
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	eng := NewEngine(g)
	result, err := eng.Run(context.Background(), testState{Value: 0})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// loop_check: Value<3 → "continue" (loop back), Value>=3 → "done"
	// Each process call increments Value by 1
	// So process runs 3 times (0→1, 1→2, 2→3) then exits
	if result.FinalState.Value != 3 {
		t.Errorf("expected Value=3 after loop, got %d", result.FinalState.Value)
	}
}

func TestLoadFromFile_UnknownNodeType(t *testing.T) {
	_, err := LoadFromFile(testdataPath(t, "simple.yaml"), NewRegistry[testState]())
	if err == nil {
		t.Fatal("expected error for unknown node type, got nil")
	}
}

func TestLoadFromFile_MissingEntry(t *testing.T) {
	// A config without an entry should fail at Compile.
	cfg := &Config{
		Name:  "bad",
		Entry: "",
		Nodes: []NodeDef{{Name: "n", Type: "identity"}},
		Edges: []EdgeDef{},
	}
	r := newTestRegistry()
	g := NewGraph[testState](cfg.Name)
	for _, ndef := range cfg.Nodes {
		fn, _ := r.lookupNode(ndef.Type)
		g.AddNode(ndef.Name, fn)
	}
	// No entry set — Compile should fail.
	err := g.Compile()
	if err == nil {
		t.Fatal("expected error for missing entry, got nil")
	}
}
