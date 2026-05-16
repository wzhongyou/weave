package checkpoint

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// InMemoryManager stores checkpoints in memory; suitable for tests and short-lived runs.
type InMemoryManager struct {
	mu   sync.Mutex
	data map[string]*Checkpoint
}

// NewInMemoryManager creates an empty in-memory checkpoint store.
func NewInMemoryManager() *InMemoryManager {
	return &InMemoryManager{data: make(map[string]*Checkpoint)}
}

func (m *InMemoryManager) Save(_ context.Context, cp *Checkpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[cp.ID] = cp
	return nil
}

func (m *InMemoryManager) Load(_ context.Context, id string) (*Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp, ok := m.data[id]
	if !ok {
		return nil, fmt.Errorf("checkpoint %q not found", id)
	}
	return cp, nil
}

func (m *InMemoryManager) List(_ context.Context, graphName string) ([]Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var infos []Info
	for _, cp := range m.data {
		if cp.GraphName == graphName {
			infos = append(infos, Info{ID: cp.ID, GraphName: cp.GraphName, Timestamp: cp.Timestamp})
		}
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Timestamp.After(infos[j].Timestamp)
	})
	return infos, nil
}

func (m *InMemoryManager) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, id)
	return nil
}

func (m *InMemoryManager) GetLatest(ctx context.Context, graphName string) (*Checkpoint, error) {
	infos, err := m.List(ctx, graphName)
	if err != nil || len(infos) == 0 {
		return nil, err
	}
	return m.Load(ctx, infos[0].ID)
}
