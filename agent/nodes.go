package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wzhongyou/graphflow/graph"
)

// LLMNodeConfig configures an LLMNode.
type LLMNodeConfig struct {
	Model        LLMModel
	SystemPrompt string
	Tools        []Tool
	Temperature  *float64
	MaxTokens    *int
	ThinkingType string // "disabled" to disable reasoning/thinking mode
	Stream       bool
	OnChunk      func(*StreamChunk)        // called for each streaming chunk
	StructuredOutput *StructuredOutputConfig // structured output config
}

// LLMNode calls an LLM and appends the assistant reply to MessageState.Messages.
type LLMNode struct{ cfg LLMNodeConfig }

// NewLLMNode creates an LLMNode with the given config.
func NewLLMNode(cfg LLMNodeConfig) *LLMNode { return &LLMNode{cfg: cfg} }

// Run implements graph.NodeFunc[*MessageState].
func (n *LLMNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	messages := n.buildMessages(s)
	req := &ChatRequest{
		Messages:     messages,
		Tools:        ToolDefs(n.cfg.Tools),
		Temperature:  n.cfg.Temperature,
		MaxTokens:    n.cfg.MaxTokens,
		ThinkingType: n.cfg.ThinkingType,
	}

	// If structured output is configured, add JSON schema to system prompt
	if n.cfg.StructuredOutput != nil {
		req.ResponseFormat = n.cfg.StructuredOutput.Schema
	}

	var resp *ChatResponse
	var err error
	if n.cfg.Stream {
		resp, err = n.chatStream(ctx, req)
	} else {
		resp, err = n.cfg.Model.Chat(ctx, req)
	}
	if err != nil {
		return s, err
	}

	// Validate structured output if configured
	if n.cfg.StructuredOutput != nil && resp.Content != "" && len(resp.ToolCalls) == 0 {
		if _, err := ValidateStructuredOutput(resp.Content, n.cfg.StructuredOutput.Schema); err != nil {
			return s, fmt.Errorf("structured output: %w", err)
		}
	}

	msg := Message{
		Role:             RoleAssistant,
		Content:          resp.Content,
		ReasoningContent: resp.ReasoningContent,
		ToolCalls:        resp.ToolCalls,
		Timestamp:        time.Now(),
	}
	s.Messages = append(s.Messages, msg)
	s.StepCount++
	if resp.Usage != nil {
		s.TotalTokens += resp.Usage.TotalTokens
	}
	return s, nil
}

func (n *LLMNode) buildMessages(s *MessageState) []Message {
	systemMsg := n.cfg.SystemPrompt

	// Append structured output instruction if configured
	if n.cfg.StructuredOutput != nil {
		instruction := n.cfg.StructuredOutput.BuildInstruction()
		if systemMsg != "" {
			systemMsg += "\n\n" + instruction
		} else {
			systemMsg = instruction
		}
	}

	if systemMsg == "" {
		return s.Messages
	}
	for i, m := range s.Messages {
		if m.Role == RoleSystem {
			if n.cfg.StructuredOutput != nil {
				s.Messages[i].Content += "\n\n" + n.cfg.StructuredOutput.BuildInstruction()
			}
			return s.Messages
		}
	}
	return append([]Message{{Role: RoleSystem, Content: systemMsg}}, s.Messages...)
}

func instructionOf(cfg *StructuredOutputConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.BuildInstruction()
}

func (n *LLMNode) chatStream(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	chunks, err := n.cfg.Model.ChatStream(ctx, req)
	if err != nil {
		return nil, err
	}
	var content strings.Builder
	var toolCalls []ToolCall
	var usage *Usage
	for chunk := range chunks {
		if chunk.Error != nil {
			return nil, chunk.Error
		}
		content.WriteString(chunk.Content)
		if chunk.ToolCalls != nil {
			toolCalls = chunk.ToolCalls
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if n.cfg.OnChunk != nil {
			n.cfg.OnChunk(chunk)
		}
	}
	return &ChatResponse{
		Content:   content.String(),
		ToolCalls: toolCalls,
		Usage:     usage,
	}, nil
}

// ToolNodeConfig configures the tool execution behaviour.
type ToolNodeConfig struct {
	Tools    []Tool
	Parallel bool // if true execute tools concurrently
}

// ToolNode executes pending tool calls found in the last assistant message.
type ToolNode struct {
	registry *ToolRegistry
	parallel bool
}

// NewToolNode creates a ToolNode backed by the given tools.
func NewToolNode(tools ...Tool) *ToolNode {
	r := NewToolRegistry()
	for _, t := range tools {
		r.Register(t)
	}
	return &ToolNode{registry: r}
}

// NewToolNodeWithRegistry creates a ToolNode from an existing registry.
func NewToolNodeWithRegistry(registry *ToolRegistry) *ToolNode {
	return &ToolNode{registry: registry}
}

// Run implements graph.NodeFunc[*MessageState].
func (n *ToolNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	if len(s.Messages) == 0 {
		return s, nil
	}
	last := s.Messages[len(s.Messages)-1]
	if last.Role != RoleAssistant || len(last.ToolCalls) == 0 {
		return s, nil
	}

	for _, tc := range last.ToolCalls {
		result := n.executeToolCall(ctx, tc)
		s.Messages = append(s.Messages, Message{
			Role:       RoleTool,
			Content:    result,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Timestamp:  time.Now(),
		})
	}
	return s, nil
}

func (n *ToolNode) executeToolCall(ctx context.Context, tc ToolCall) string {
	tool, ok := n.registry.Get(tc.Name)
	if !ok {
		return fmt.Sprintf("error: tool %q not found", tc.Name)
	}
	result, err := tool.Execute(ctx, tc.Arguments)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return result
}

// VectorRetrieveNode embeds the last user message and retrieves relevant documents.
type VectorRetrieveNode struct {
	Embedder    Embedder
	VectorStore VectorStore
	TopK        int
}

// Run implements graph.NodeFunc[*MessageState].
func (n *VectorRetrieveNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	if len(s.Messages) == 0 || n.Embedder == nil || n.VectorStore == nil {
		return s, nil
	}

	last := s.Messages[len(s.Messages)-1]
	vector, err := n.Embedder.Embed(ctx, last.Content)
	if err != nil {
		return s, fmt.Errorf("embedding: %w", err)
	}

	results, err := n.VectorStore.Search(ctx, vector, n.TopK)
	if err != nil {
		return s, fmt.Errorf("vector search: %w", err)
	}

	if s.Context == nil {
		s.Context = make(map[string]any)
	}
	docs := make([]string, len(results))
	for i, r := range results {
		docs[i] = r.ID
	}
	s.Context["retrieved_docs"] = results
	s.Context["retrieved_doc_ids"] = docs
	return s, nil
}

// HumanInputNode suspends the graph and waits for a human message (HITL).
type HumanInputNode struct {
	Prompt  string
	Timeout time.Duration
}

// Run implements graph.NodeFunc[*MessageState].
func (n *HumanInputNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	// HITL requires an external channel/checkpoint mechanism; stub for now.
	return s, nil
}

// ── Supervisor Node Types ──────────────────────────────────────────────────

// supervisorRouteNode executes the sub-agent's graph for the routing target.
type supervisorRouteNode struct {
	subAgents map[string]SubAgent
}

// Run implements graph.NodeFunc[*MessageState].
func (n *supervisorRouteNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	if len(s.Messages) == 0 {
		return s, nil
	}
	last := s.Messages[len(s.Messages)-1]
	if len(last.ToolCalls) == 0 {
		return s, nil
	}

	tc := last.ToolCalls[0]
	agentName := tc.Name

	// "complete" or "final_answer" means done
	if agentName == "complete" || agentName == "final_answer" {
		s.CurrentAgent = ""
		s.NextAgent = ""
		return s, nil
	}

	subAgent, ok := n.subAgents[agentName]
	if !ok {
		s.Messages = append(s.Messages, Message{
			Role:       RoleTool,
			Content:    fmt.Sprintf("未知的子智能体: %s", agentName),
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Timestamp:  time.Now(),
		})
		return s, nil
	}

	s.CurrentAgent = agentName
	s.NextAgent = agentName

	// Build and run sub-agent's graph with the current state
	subGraph, subErr := subAgent.BuildGraph()
	if subErr != nil {
		s.Messages = append(s.Messages, Message{
			Role:       RoleTool,
			Content:    fmt.Sprintf("构建子智能体 %s 失败: %v", agentName, subErr),
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Timestamp:  time.Now(),
		})
		return s, nil
	}

	subEngine := graph.NewEngine(subGraph)
	subResult, subErr := subEngine.Run(ctx, s)
	if subErr != nil {
		s.Messages = append(s.Messages, Message{
			Role:       RoleTool,
			Content:    fmt.Sprintf("运行子智能体 %s 失败: %v", agentName, subErr),
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Timestamp:  time.Now(),
		})
		return s, nil
	}

	// Merge sub-agent results back
	*s = *subResult.FinalState
	s.CompletedAgents = append(s.CompletedAgents, agentName)
	s.CurrentAgent = ""
	s.NextAgent = ""

	return s, nil
}

// collectNode is a passthrough that aggregates results before returning to the supervisor.
type collectNode struct{}

// Run implements graph.NodeFunc[*MessageState].
func (n *collectNode) Run(ctx context.Context, s *MessageState) (*MessageState, error) {
	// State has already been updated by supervisorRouteNode.
	// This node exists to provide a clear graph structure.
	return s, nil
}
