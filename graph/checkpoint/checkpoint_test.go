package checkpoint_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wzhongyou/graphflow/graph/checkpoint"
)

func makeCheckpoint(id, graph string) *checkpoint.Checkpoint {
	return &checkpoint.Checkpoint{
		ID:        id,
		GraphName: graph,
		State:     []byte(`{"val":1}`),
		StateType: "intState",
		Timestamp: time.Now(),
	}
}

func runSuite(t *testing.T, mgr checkpoint.Manager) {
	t.Helper()
	ctx := context.Background()

	// Save two checkpoints for the same graph.
	cp1 := makeCheckpoint("cp-1", "my-graph")
	cp2 := makeCheckpoint("cp-2", "my-graph")
	time.Sleep(time.Millisecond) // ensure distinct timestamps
	cp2.Timestamp = cp1.Timestamp.Add(time.Millisecond)

	if err := mgr.Save(ctx, cp1); err != nil {
		t.Fatalf("Save cp1: %v", err)
	}
	if err := mgr.Save(ctx, cp2); err != nil {
		t.Fatalf("Save cp2: %v", err)
	}

	// Load by ID.
	got, err := mgr.Load(ctx, "cp-1")
	if err != nil {
		t.Fatalf("Load cp-1: %v", err)
	}
	if got.ID != "cp-1" {
		t.Errorf("want cp-1, got %q", got.ID)
	}

	// Load unknown ID returns error.
	if _, err := mgr.Load(ctx, "ghost"); err == nil {
		t.Error("expected error for unknown ID")
	}

	// List returns both, newest first.
	infos, err := mgr.List(ctx, "my-graph")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("want 2 infos, got %d", len(infos))
	}
	if infos[0].ID != "cp-2" {
		t.Errorf("want cp-2 first (newest), got %q", infos[0].ID)
	}

	// GetLatest returns cp-2.
	latest, err := mgr.GetLatest(ctx, "my-graph")
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if latest.ID != "cp-2" {
		t.Errorf("want latest=cp-2, got %q", latest.ID)
	}

	// Different graph name returns empty list.
	others, err := mgr.List(ctx, "other-graph")
	if err != nil {
		t.Fatalf("List other: %v", err)
	}
	if len(others) != 0 {
		t.Errorf("want 0 for other-graph, got %d", len(others))
	}

	// Delete cp-1.
	if err := mgr.Delete(ctx, "cp-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	infos, _ = mgr.List(ctx, "my-graph")
	if len(infos) != 1 {
		t.Errorf("want 1 after delete, got %d", len(infos))
	}
}

func TestInMemoryManager(t *testing.T) {
	runSuite(t, checkpoint.NewInMemoryManager())
}

func TestFileManager(t *testing.T) {
	dir, err := os.MkdirTemp("", "graphflow-cp-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	runSuite(t, checkpoint.NewFileManager(dir))
}

func TestFileManager_EmptyDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "graphflow-cp-empty-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	mgr := checkpoint.NewFileManager(dir)
	ctx := context.Background()

	infos, err := mgr.List(ctx, "any")
	if err != nil {
		t.Fatalf("List on empty dir: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("want 0, got %d", len(infos))
	}

	latest, err := mgr.GetLatest(ctx, "any")
	if err != nil {
		t.Fatalf("GetLatest on empty: %v", err)
	}
	if latest != nil {
		t.Errorf("want nil, got %v", latest)
	}
}
