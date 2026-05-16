// agent_demo shows how to build a minimal ReAct agent using graphflow,
// including how to observe each Thought / Action / Observation step via Hook.
// Run: go run ./examples/agent_demo
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/wzhongyou/graphflow/agent"
	"github.com/wzhongyou/graphflow/graph"
)

func main() {
	ctx := context.Background()

	// 1. Wire up LLM and tools.
	llm := agent.NewLLMNode(agent.LLMNodeConfig{
		Model:        &mockLLM{},
		SystemPrompt: "You are a helpful assistant.",
	})
	tools := agent.NewToolRegistry()
	tools.Register(&calculatorTool{})
	toolNode := agent.NewToolNode(tools.List()...)

	// 2. Build the ReAct graph: llm -> tool -> llm (loop until no tool calls).
	g := graph.NewGraph[*agent.MessageState]("react-agent")
	g.AddNode("llm", llm.Run)
	g.AddNode("tool", toolNode.Run)
	g.SetEntryPoint("llm")
	g.AddCondition("llm", graph.Condition[*agent.MessageState]{
		If:     hasPendingToolCalls,
		Target: "tool",
	})
	g.AddEdge("tool", "llm")
	g.SetMaxIterations("llm", 20)

	if err := g.Compile(); err != nil {
		log.Fatal(err)
	}

	// 3. Compose hooks.
	//    ReactHook prints the Thought/Action/Observation chain to stdout.
	//    You can add more hooks (e.g. OTel, logging) via ComposeHooks.
	hook := graph.ComposeHooks(
		&reactHook{},
		// graph.WithHook(&myOTelHook{}) ← add more here
	)

	// 4. Run with hook attached.
	engine := graph.NewEngine(g)
	result, err := engine.Run(ctx, &agent.MessageState{
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: "What is 123 * 456?"},
		},
		MaxSteps: 20,
	}, graph.WithHook(hook))
	if err != nil {
		log.Fatal(err)
	}

	last := result.FinalState.Messages[len(result.FinalState.Messages)-1]
	fmt.Printf("\n[done] steps=%d duration=%s\n", result.TotalSteps, result.TotalDuration.Round(time.Millisecond))
	fmt.Println("Assistant:", last.Content)
}

// ── ReAct hook ────────────────────────────────────────────────────────────────
//
// reactHook implements graph.Hook[*agent.MessageState].
// It prints each Thought (LLM decision), Action (tool call), and
// Observation (tool result) as they happen — the canonical ReAct trace.

type reactHook struct{}

func (h *reactHook) OnGraphStart(_ context.Context, name string, s *agent.MessageState) {
	if len(s.Messages) > 0 {
		fmt.Printf("[%s] question: %s\n", name, s.Messages[len(s.Messages)-1].Content)
	}
}

func (h *reactHook) OnGraphEnd(_ context.Context, _ string, _ *agent.MessageState, err error) {
	if err != nil {
		fmt.Printf("[graph] failed: %v\n", err)
	}
}

func (h *reactHook) OnNodeStart(_ context.Context, nodeName string, _ *agent.MessageState) {
	fmt.Printf("  → [%s] ...\n", nodeName)
}

// OnNodeEnd is where the interesting ReAct tracing happens:
// after the llm node we see Thought / tool call decision,
// after the tool node we see the Observation.
func (h *reactHook) OnNodeEnd(_ context.Context, nodeName string, s *agent.MessageState, err error, dur time.Duration) {
	if err != nil {
		fmt.Printf("  ✗ [%s] error: %v (%s)\n", nodeName, err, dur)
		return
	}
	if len(s.Messages) == 0 {
		return
	}
	last := s.Messages[len(s.Messages)-1]

	switch nodeName {
	case "llm":
		if len(last.ToolCalls) > 0 {
			// Thought → Action
			calls := make([]string, len(last.ToolCalls))
			for i, tc := range last.ToolCalls {
				calls[i] = fmt.Sprintf("%s(%v)", tc.Name, tc.Arguments)
			}
			fmt.Printf("  [thought] → Action: %s  (%s)\n", strings.Join(calls, ", "), dur)
		} else {
			// Thought → Final Answer
			fmt.Printf("  [thought] → Answer: %s  (%s)\n", last.Content, dur)
		}

	case "tool":
		// Observation: find the tool result messages appended after the tool calls
		for _, msg := range s.Messages {
			if msg.Role == agent.RoleTool {
				fmt.Printf("  [observation] %s: %s\n", msg.ToolName, msg.Content)
			}
		}
	}
}

func (h *reactHook) OnRetry(_ context.Context, nodeName string, attempt int, lastErr error) {
	fmt.Printf("  ↺ [%s] retry %d: %v\n", nodeName, attempt, lastErr)
}

// ── Condition ─────────────────────────────────────────────────────────────────

func hasPendingToolCalls(_ context.Context, s *agent.MessageState) bool {
	if len(s.Messages) == 0 {
		return false
	}
	last := s.Messages[len(s.Messages)-1]
	return last.Role == agent.RoleAssistant && len(last.ToolCalls) > 0
}

// ── Mock LLM (replace with a real client) ────────────────────────────────────

type mockLLM struct{}

func (m *mockLLM) Chat(_ context.Context, msgs []agent.Message) (*agent.Message, error) {
	for _, msg := range msgs {
		if msg.Role == agent.RoleTool {
			return &agent.Message{Role: agent.RoleAssistant, Content: "The answer is 56088."}, nil
		}
	}
	return &agent.Message{
		Role: agent.RoleAssistant,
		ToolCalls: []agent.ToolCall{
			{ID: "c1", Name: "calculator", Arguments: map[string]any{"expr": "123 * 456"}},
		},
	}, nil
}

func (m *mockLLM) ChatStream(_ context.Context, _ []agent.Message) (<-chan string, error) {
	return nil, nil
}

// ── Calculator tool ───────────────────────────────────────────────────────────

type calculatorTool struct{}

func (c *calculatorTool) Name() string        { return "calculator" }
func (c *calculatorTool) Description() string { return "Evaluates a simple arithmetic expression." }
func (c *calculatorTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"expr": map[string]any{"type": "string", "description": "e.g. 123 * 456"},
		},
		"required": []string{"expr"},
	}
}
func (c *calculatorTool) Execute(_ context.Context, args map[string]any) (string, error) {
	return fmt.Sprintf("result of %q = 56088 (stub)", args["expr"]), nil
}
