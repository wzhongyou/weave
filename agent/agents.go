package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/wzhongyou/graphflow/graph"
)

// ── ReActAgent ──────────────────────────────────────────────────────────────────

// ReActAgentConfig configures a ReAct-style agent.
type ReActAgentConfig struct {
	Name         string
	LLM          LLMModel
	SystemPrompt string
	Tools        []Tool
	MaxSteps     int
}

// ReActAgent builds a Reason-Act loop graph.
type ReActAgent struct{ cfg ReActAgentConfig }

// NewReActAgent creates a ReActAgent.
func NewReActAgent(cfg ReActAgentConfig) *ReActAgent {
	if cfg.Name == "" {
		cfg.Name = "react-agent"
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 20
	}
	return &ReActAgent{cfg: cfg}
}

// Name returns the agent's name (implements SubAgent).
func (a *ReActAgent) Name() string { return a.cfg.Name }

// BuildGraph constructs and compiles the ReAct graph.
// Structure: llm ──(has tool calls)──→ tool ──→ llm (loop)
func (a *ReActAgent) BuildGraph() (*graph.Graph[*MessageState], error) {
	llmNode := NewLLMNode(LLMNodeConfig{
		Model:        a.cfg.LLM,
		SystemPrompt: a.cfg.SystemPrompt,
		Tools:        a.cfg.Tools,
	})
	toolNode := NewToolNode(a.cfg.Tools...)

	g := graph.NewGraph[*MessageState](a.cfg.Name)
	g.AddNode("llm", llmNode.Run)
	g.AddNode("tool", toolNode.Run)
	g.SetEntryPoint("llm")

	g.AddCondition("llm", graph.Condition[*MessageState]{
		If:     HasPendingToolCalls,
		Target: "tool",
	})

	g.AddEdge("tool", "llm")
	g.SetMaxIterations("llm", a.cfg.MaxSteps)

	if err := g.Compile(); err != nil {
		return nil, fmt.Errorf("react agent: %w", err)
	}
	return g, nil
}

// HasPendingToolCalls returns true when the last assistant message contains
// tool calls that need executing.
func HasPendingToolCalls(_ context.Context, s *MessageState) bool {
	if len(s.Messages) == 0 {
		return false
	}
	last := s.Messages[len(s.Messages)-1]
	return last.Role == RoleAssistant && len(last.ToolCalls) > 0
}

// ── RAGAgent ────────────────────────────────────────────────────────────────────

// RAGAgentConfig configures a Retrieval-Augmented Generation agent.
type RAGAgentConfig struct {
	Name         string
	LLM          LLMModel
	Embedder     Embedder
	VectorStore  VectorStore
	SystemPrompt string
	TopK         int
}

// RAGAgent builds a retrieve-then-generate graph.
type RAGAgent struct{ cfg RAGAgentConfig }

// NewRAGAgent creates a RAGAgent.
func NewRAGAgent(cfg RAGAgentConfig) *RAGAgent {
	if cfg.Name == "" {
		cfg.Name = "rag-agent"
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	return &RAGAgent{cfg: cfg}
}

// Name returns the agent's name (implements SubAgent).
func (a *RAGAgent) Name() string { return a.cfg.Name }

// BuildGraph constructs and compiles the RAG graph.
// Structure: retrieve → llm
func (a *RAGAgent) BuildGraph() (*graph.Graph[*MessageState], error) {
	retrieveNode := &VectorRetrieveNode{
		Embedder:    a.cfg.Embedder,
		VectorStore: a.cfg.VectorStore,
		TopK:        a.cfg.TopK,
	}
	llmNode := NewLLMNode(LLMNodeConfig{
		Model:        a.cfg.LLM,
		SystemPrompt: a.cfg.SystemPrompt,
	})

	g := graph.NewGraph[*MessageState](a.cfg.Name)
	g.AddNode("retrieve", retrieveNode.Run)
	g.AddNode("llm", llmNode.Run)
	g.SetEntryPoint("retrieve")
	g.AddEdge("retrieve", "llm")

	if err := g.Compile(); err != nil {
		return nil, fmt.Errorf("rag agent: %w", err)
	}
	return g, nil
}

// ── SupervisorAgent ─────────────────────────────────────────────────────────────

// SupervisorAgentConfig configures a multi-agent supervisor.
type SupervisorAgentConfig struct {
	Name      string
	LLM       LLMModel
	SubAgents map[string]SubAgent
	MaxRounds int
}

// SubAgent is implemented by any agent that can be orchestrated by a supervisor.
type SubAgent interface {
	BuildGraph() (*graph.Graph[*MessageState], error)
	Name() string
}

// SupervisorAgent routes tasks to sub-agents and aggregates results.
type SupervisorAgent struct{ cfg SupervisorAgentConfig }

// NewSupervisorAgent creates a SupervisorAgent.
func NewSupervisorAgent(cfg SupervisorAgentConfig) *SupervisorAgent {
	if cfg.Name == "" {
		cfg.Name = "supervisor-agent"
	}
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 10
	}
	return &SupervisorAgent{cfg: cfg}
}

// BuildGraph constructs the supervisor orchestration graph.
// Structure: supervisor_llm → [route] → sub-agent → collect → supervisor_llm (loop)
func (a *SupervisorAgent) BuildGraph() (*graph.Graph[*MessageState], error) {
	agentNames := make([]string, 0, len(a.cfg.SubAgents))
	for name := range a.cfg.SubAgents {
		agentNames = append(agentNames, name)
	}

	supervisorSystemPrompt := fmt.Sprintf(`你是一个管理者智能体，负责将任务分派给子智能体。

可用的子智能体：
%s

使用 "route" 工具将任务路由给合适的子智能体。
当所有子智能体完成任务后，直接给用户最终回复，不需要再调用工具。`, strings.Join(agentNames, "\n"))

	llmNode := NewLLMNode(LLMNodeConfig{
		Model:        a.cfg.LLM,
		SystemPrompt: supervisorSystemPrompt,
		Tools:        []Tool{&routeTool{subAgents: a.cfg.SubAgents}},
	})

	g := graph.NewGraph[*MessageState](a.cfg.Name)
	g.AddNode("supervisor", llmNode.Run)
	g.AddNode("route", (&supervisorRouteNode{subAgents: a.cfg.SubAgents}).Run)
	g.AddNode("collect", (&collectNode{}).Run)
	g.SetEntryPoint("supervisor")

	// If supervisor makes tool calls → route to sub-agent
	g.AddCondition("supervisor", graph.Condition[*MessageState]{
		If:     HasPendingToolCalls,
		Target: "route",
	})

	// Route → collect → supervisor (loop back)
	g.AddEdge("route", "collect")
	g.AddEdge("collect", "supervisor")

	g.SetMaxIterations("supervisor", a.cfg.MaxRounds)

	if err := g.Compile(); err != nil {
		return nil, fmt.Errorf("supervisor agent: %w", err)
	}
	return g, nil
}

// routeTool is a tool the supervisor uses to route to a sub-agent.
type routeTool struct {
	subAgents map[string]SubAgent
}

func (t *routeTool) Name() string { return "route" }
func (t *routeTool) Description() string {
	return "将当前任务路由给指定的子智能体来处理"
}
func (t *routeTool) Parameters() map[string]any {
	agents := make([]string, 0, len(t.subAgents))
	for name := range t.subAgents {
		agents = append(agents, name)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent": map[string]any{
				"type":        "string",
				"enum":        agents,
				"description": "要路由到的子智能体名称",
			},
		},
		"required": []string{"agent"},
	}
}
func (t *routeTool) Execute(_ context.Context, args map[string]any) (string, error) {
	agent, ok := args["agent"].(string)
	if !ok {
		return "", fmt.Errorf("route: 'agent' 参数必须为字符串")
	}
	if _, exists := t.subAgents[agent]; !exists {
		return "", fmt.Errorf("route: 未知子智能体 %q", agent)
	}
	return fmt.Sprintf("正在路由到子智能体: %s", agent), nil
}
