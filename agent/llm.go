package agent

import "context"

// LLMModel is the interface every LLM backend must satisfy.
type LLMModel interface {
	// Chat sends a list of messages and returns the assistant reply.
	Chat(ctx context.Context, messages []Message) (*Message, error)

	// ChatStream sends messages and streams token-by-token output.
	ChatStream(ctx context.Context, messages []Message) (<-chan string, error)
}

// Embedder converts text into a dense vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// VectorStore stores and retrieves embedded documents.
type VectorStore interface {
	Insert(ctx context.Context, id string, vector []float32, metadata map[string]any) error
	Search(ctx context.Context, query []float32, topK int) ([]SearchResult, error)
}

// SearchResult is a single hit from a vector search.
type SearchResult struct {
	ID       string         `json:"id"`
	Score    float32        `json:"score"`
	Metadata map[string]any `json:"metadata"`
}

// TODO(A2): add function-calling / tool-use support to LLMModel
