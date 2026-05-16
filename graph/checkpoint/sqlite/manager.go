// Package sqlite provides a SQLite-backed checkpoint.Manager.
// Import: github.com/wzhongyou/graphflow/graph/checkpoint/sqlite
//
// Dependency: github.com/mattn/go-sqlite3 (add to go.mod when implementing)
package sqlite

import "context"

// TODO(P6): implement SQLiteManager
//   Schema:
//     CREATE TABLE checkpoints (
//       id         TEXT PRIMARY KEY,
//       graph_name TEXT NOT NULL,
//       state      BLOB NOT NULL,
//       state_type TEXT NOT NULL,
//       completed  TEXT NOT NULL,  -- JSON array
//       queued     TEXT NOT NULL,  -- JSON array
//       iter_count TEXT NOT NULL,  -- JSON object
//       created_at INTEGER NOT NULL
//     );
//     CREATE INDEX idx_cp_graph ON checkpoints(graph_name, created_at DESC);

// Manager stores checkpoints in a local SQLite database.
type Manager struct {
	// db *sql.DB  // uncomment when implementing
	path string
}

// NewManager opens (or creates) a SQLite database at path.
func NewManager(path string) *Manager { return &Manager{path: path} }

func (m *Manager) Save(_ context.Context, _ any) error                { return nil }
func (m *Manager) Load(_ context.Context, _ string) (any, error)      { return nil, nil }
func (m *Manager) List(_ context.Context, _ string) (any, error)      { return nil, nil }
func (m *Manager) Delete(_ context.Context, _ string) error           { return nil }
func (m *Manager) GetLatest(_ context.Context, _ string) (any, error) { return nil, nil }
