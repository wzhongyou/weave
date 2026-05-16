// Package agent provides AI Agent abstractions built on top of the graphflow core engine.
package agent

import "time"

// Role identifies the speaker of a Message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

// Message is a single entry in a conversation.
type Message struct {
	ID        string         `json:"id"`
	Role      Role           `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []ToolCall     `json:"tool_calls,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// ToolCall represents a single tool invocation and its result.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Result    string         `json:"result,omitempty"`
}

// MessageState is the standard graph state for Agent workflows.
type MessageState struct {
	Messages        []Message      `json:"messages"`
	Context         map[string]any `json:"context"`
	CurrentAgent    string         `json:"current_agent"`
	CompletedAgents []string       `json:"completed_agents"`
	NextAgent       string         `json:"next_agent"`
	StepCount       int            `json:"step_count"`
	MaxSteps        int            `json:"max_steps"`
	Metadata        map[string]any `json:"metadata"`
}
