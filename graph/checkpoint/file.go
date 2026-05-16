package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileManager persists checkpoints as JSON files in a directory.
// Suitable for single-machine deployments with no external dependencies.
type FileManager struct {
	dir string
}

// NewFileManager creates a FileManager that stores checkpoints in dir.
func NewFileManager(dir string) *FileManager { return &FileManager{dir: dir} }

func (m *FileManager) Save(_ context.Context, cp *Checkpoint) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("create checkpoint dir: %w", err)
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	return os.WriteFile(filepath.Join(m.dir, cp.ID+".json"), data, 0o644)
}

func (m *FileManager) Load(_ context.Context, id string) (*Checkpoint, error) {
	data, err := os.ReadFile(filepath.Join(m.dir, id+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("checkpoint %q not found", id)
		}
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}
	return &cp, nil
}

func (m *FileManager) List(ctx context.Context, graphName string) ([]Info, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var infos []Info
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		cp, err := m.Load(ctx, id)
		if err != nil || cp.GraphName != graphName {
			continue
		}
		infos = append(infos, Info{ID: cp.ID, GraphName: cp.GraphName, Timestamp: cp.Timestamp})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Timestamp.After(infos[j].Timestamp)
	})
	return infos, nil
}

func (m *FileManager) Delete(_ context.Context, id string) error {
	return os.Remove(filepath.Join(m.dir, id+".json"))
}

func (m *FileManager) GetLatest(ctx context.Context, graphName string) (*Checkpoint, error) {
	infos, err := m.List(ctx, graphName)
	if err != nil || len(infos) == 0 {
		return nil, err
	}
	return m.Load(ctx, infos[0].ID)
}
