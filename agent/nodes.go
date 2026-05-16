package agent

import (
	"context"
	"time"
)

// LLMNodeConfig configures an LLMNode.
type LLMNodeConfig struct {
	Model        LLMModel
	SystemPrompt string
	Stream       bool
}

// LLMNode calls an LLM and appends the assistant reply to MessageState.Messages.
type LLMNode struct{ cfg LLMNodeConfig }

// NewLLMNode creates an LLMNode with the given config.
func NewLLMNode(cfg LLMNodeConfig) *LLMNode { return &LLMNode{cfg: cfg} }

// Run implements graphflow.NodeFunc[*MessageState].
func (n *LLMNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	// TODO(A3): call n.cfg.Model.Chat / ChatStream, append reply to s.Messages
	return s, nil
}

// ToolNode executes tool calls found in the last assistant message.
type ToolNode struct{ tools []Tool }

// NewToolNode creates a ToolNode backed by the given tools.
func NewToolNode(tools ...Tool) *ToolNode { return &ToolNode{tools: tools} }

// Run implements graphflow.NodeFunc[*MessageState].
func (n *ToolNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	// TODO(A3): find pending ToolCalls in last message, dispatch, append results
	return s, nil
}

// VectorRetrieveNode retrieves context documents and appends them to MessageState.Context.
type VectorRetrieveNode struct {
	Embedder    Embedder
	VectorStore VectorStore
	TopK        int
}

// Run implements graphflow.NodeFunc[*MessageState].
func (n *VectorRetrieveNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	// TODO(A3): embed last user message, search vector store, inject results into context
	return s, nil
}

// HumanInputNode suspends the graph and waits for a human message (HITL).
type HumanInputNode struct {
	Prompt  string
	Timeout time.Duration
}

// Run implements graphflow.NodeFunc[*MessageState].
func (n *HumanInputNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	// TODO(A3): block on a channel or checkpoint until human reply arrives
	return s, nil
}
