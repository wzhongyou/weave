package checkpoint

import (
	"context"
	"time"
)

// Checkpoint captures the full execution state of a graph at a point in time.
type Checkpoint struct {
	ID        string         `json:"id"`
	GraphName string         `json:"graph_name"`
	State     []byte         `json:"state"`
	StateType string         `json:"state_type"`
	Completed []string       `json:"completed"`
	Queued    []string       `json:"queued"`
	IterCount map[string]int `json:"iter_count"`
	Timestamp time.Time      `json:"timestamp"`
}

// Info is a lightweight summary returned by Manager.List.
type Info struct {
	ID        string    `json:"id"`
	GraphName string    `json:"graph_name"`
	Timestamp time.Time `json:"timestamp"`
}

// Manager is the storage interface for checkpoints.
type Manager interface {
	Save(ctx context.Context, cp *Checkpoint) error
	Load(ctx context.Context, id string) (*Checkpoint, error)
	List(ctx context.Context, graphName string) ([]Info, error)
	Delete(ctx context.Context, id string) error
	GetLatest(ctx context.Context, graphName string) (*Checkpoint, error)
}
