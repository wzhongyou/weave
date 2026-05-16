// Package redis provides a Redis-backed checkpoint.Manager.
// Import: github.com/wzhongyou/graphflow/graph/checkpoint/redis
//
// Dependency: github.com/redis/go-redis/v9 (add to go.mod when implementing)
package redis

import "context"

// TODO(P6): implement RedisManager
//   - Store checkpoints as JSON hashes under key "graphflow:cp:<id>"
//   - Use ZADD graphflow:index:<graphName> <unix-ts> <id> for List / GetLatest
//   - All operations must be safe for concurrent callers

// Manager stores checkpoints in Redis.
type Manager struct {
	// client redis.UniversalClient  // uncomment when implementing
	keyPrefix string
}

// NewManager creates a RedisManager connecting to addr (e.g. "localhost:6379").
func NewManager(addr, keyPrefix string) *Manager {
	return &Manager{keyPrefix: keyPrefix}
}

// Implement checkpoint.Manager — stubs satisfy the interface at compile time.

type Checkpoint = struct{ ID string } // placeholder; use checkpoint.Checkpoint in impl

func (m *Manager) Save(_ context.Context, _ any) error           { return nil }
func (m *Manager) Load(_ context.Context, _ string) (any, error) { return nil, nil }
func (m *Manager) List(_ context.Context, _ string) (any, error) { return nil, nil }
func (m *Manager) Delete(_ context.Context, _ string) error      { return nil }
func (m *Manager) GetLatest(_ context.Context, _ string) (any, error) { return nil, nil }
