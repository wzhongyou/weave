package agent

import "github.com/wzhongyou/graphflow/graph"

// ReActAgentConfig configures a ReAct-style agent.
type ReActAgentConfig struct {
	LLM      LLMModel
	Tools    []Tool
	MaxSteps int
}

// ReActAgent builds a Reason-Act loop graph.
type ReActAgent struct{ cfg ReActAgentConfig }

// NewReActAgent creates a ReActAgent.
func NewReActAgent(cfg ReActAgentConfig) *ReActAgent { return &ReActAgent{cfg: cfg} }

// BuildGraph constructs and compiles the ReAct graph.
func (a *ReActAgent) BuildGraph() (*graph.Graph[*MessageState], error) {
	// TODO(A6): llm_node -> tool_node -> (loop back to llm_node until done)
	return nil, nil
}

// RAGAgentConfig configures a Retrieval-Augmented Generation agent.
type RAGAgentConfig struct {
	LLM         LLMModel
	Embedder    Embedder
	VectorStore VectorStore
	TopK        int
}

// RAGAgent builds a retrieve-then-generate graph.
type RAGAgent struct{ cfg RAGAgentConfig }

// NewRAGAgent creates a RAGAgent.
func NewRAGAgent(cfg RAGAgentConfig) *RAGAgent { return &RAGAgent{cfg: cfg} }

// BuildGraph constructs and compiles the RAG graph.
func (a *RAGAgent) BuildGraph() (*graph.Graph[*MessageState], error) {
	// TODO(A7): retrieve_node -> llm_node
	return nil, nil
}

// SupervisorAgentConfig configures a multi-agent supervisor.
type SupervisorAgentConfig struct {
	LLM       LLMModel
	SubAgents map[string]SubAgent
	MaxRounds int
}

// SubAgent is implemented by any agent that can be orchestrated by a supervisor.
type SubAgent interface {
	BuildGraph() (*graph.Graph[*MessageState], error)
}

// SupervisorAgent routes tasks to sub-agents and aggregates results.
type SupervisorAgent struct{ cfg SupervisorAgentConfig }

// NewSupervisorAgent creates a SupervisorAgent.
func NewSupervisorAgent(cfg SupervisorAgentConfig) *SupervisorAgent {
	return &SupervisorAgent{cfg: cfg}
}

// BuildGraph constructs the supervisor orchestration graph.
func (a *SupervisorAgent) BuildGraph() (*graph.Graph[*MessageState], error) {
	// TODO(A8): supervisor_llm -> route -> sub-agent subgraphs -> aggregate
	return nil, nil
}
