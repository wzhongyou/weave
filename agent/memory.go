package agent

import "context"

// ShortTermMemory keeps the recent conversation window in memory.
type ShortTermMemory struct {
	maxMessages int
	messages    []Message
}

// NewShortTermMemory creates a memory buffer capped at maxMessages.
func NewShortTermMemory(maxMessages int) *ShortTermMemory {
	return &ShortTermMemory{maxMessages: maxMessages}
}

// Add appends a message, evicting the oldest if the buffer is full.
func (m *ShortTermMemory) Add(msg Message) {
	// TODO(A5): ring-buffer eviction
}

// Messages returns the current message window.
func (m *ShortTermMemory) Messages() []Message { return m.messages }

// LongTermMemory persists and retrieves memories via a VectorStore.
type LongTermMemory struct {
	embedder    Embedder
	vectorStore VectorStore
}

// NewLongTermMemory creates a long-term memory backed by the given stores.
func NewLongTermMemory(embedder Embedder, store VectorStore) *LongTermMemory {
	return &LongTermMemory{embedder: embedder, vectorStore: store}
}

// Remember embeds and stores a memory string.
func (m *LongTermMemory) Remember(ctx context.Context, text string, metadata map[string]any) error {
	// TODO(A5): embed text, insert into vectorStore
	return nil
}

// Recall retrieves the top-k most relevant memories for a query.
func (m *LongTermMemory) Recall(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	// TODO(A5): embed query, search vectorStore
	return nil, nil
}
