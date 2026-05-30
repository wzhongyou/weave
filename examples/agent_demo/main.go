// agent_demo shows how to build a ReAct agent using graphflow.
//
//	Mock mode (default):        go run ./examples/agent_demo
//	From config file:           go run ./examples/agent_demo -config config/llmgate.toml
//	From config + pin provider: go run ./examples/agent_demo -config config/llmgate.toml -provider deepseek
//	From env vars only:         DEEPSEEK_KEY=sk-xxx go run ./examples/agent_demo -env
//	From env vars + provider:   DEEPSEEK_KEY=sk-xxx go run ./examples/agent_demo -env -provider deepseek
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/wzhongyou/graphflow/agent"
	llmgate_adapter "github.com/wzhongyou/graphflow/agent/llmgate"
	"github.com/wzhongyou/graphflow/graph"

	"github.com/wzhongyou/llmgate/sdk"
)

var (
	configPath = flag.String("config", "", "Path to llmgate TOML config file")
	envMode    = flag.Bool("env", false, "Load providers from environment variables (DEEPSEEK_KEY, etc.)")
	provider   = flag.String("provider", "", "Provider to pin to (e.g. deepseek, glm)")
	question   = flag.String("q", "What is 123 * 456?", "User question")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	llmModel := buildLLM()
	tools := []agent.Tool{&agent.CalculatorTool{}}

	// Build and run a ReAct agent in 3 lines.
	ag := agent.NewReActAgent(agent.ReActAgentConfig{
		Name:         "react-agent",
		LLM:          llmModel,
		SystemPrompt: "You are a helpful assistant. Use the calculator tool for math.",
		Tools:        tools,
		MaxSteps:     20,
	})
	g, err := ag.BuildGraph()
	if err != nil {
		log.Fatal(err)
	}

	engine := graph.NewEngine(g)
	result, err := engine.Run(ctx, &agent.MessageState{
		Messages: []agent.Message{{Role: agent.RoleUser, Content: *question}},
		MaxSteps: 20,
	}, graph.WithHook(graph.ComposeHooks(&reactHook{})))
	if err != nil {
		log.Fatal(err)
	}

	last := result.FinalState.Messages[len(result.FinalState.Messages)-1]
	fmt.Printf("\n[done] steps=%d  duration=%s  tokens=%d\n",
		result.TotalSteps, result.TotalDuration.Round(time.Millisecond), result.FinalState.TotalTokens)
	fmt.Println("Assistant:", last.Content)
}

// ── LLM setup ──────────────────────────────────────────────────────────────────

func buildLLM() agent.LLMModel {
	// 1. Explicit config file path.
	if *configPath != "" {
		return fromConfig(*configPath)
	}

	// 2. Auto-detect config/llmgate.toml.
	if _, err := os.Stat("config/llmgate.toml"); err == nil {
		return fromConfig("config/llmgate.toml")
	}
	if _, err := os.Stat("llmgate.toml"); err == nil {
		return fromConfig("llmgate.toml")
	}

	// 3. Environment variables (DEEPSEEK_KEY etc.).
	if *envMode || hasLLMKeyEnv() {
		return fromEnv()
	}

	// 4. Fallback to mock.
	fmt.Println("💡 Using mock LLM (no config or API keys found).")
	fmt.Println("   For real models:")
	fmt.Println("     cp config/llmgate.toml.example config/llmgate.toml    # edit keys, then:")
	fmt.Println("     go run ./examples/agent_demo")
	fmt.Println("   Or use env vars:")
	fmt.Println("     DEEPSEEK_KEY=sk-xxx go run ./examples/agent_demo -env -provider deepseek")
	fmt.Println()
	return &mockLLM{}
}

func fromConfig(path string) agent.LLMModel {
	fmt.Printf("🔗 config: %s\n", path)
	gw, err := sdk.NewFromFile(path)
	if err != nil {
		log.Fatalf("llmgate: %v", err)
	}
	return adapterFromGW(gw)
}

func fromEnv() agent.LLMModel {
	fmt.Println("🔗 loading from environment variables")
	gw := sdk.New()
	return adapterFromGW(gw)
}

func adapterFromGW(gw *sdk.Gateway) agent.LLMModel {
	if *provider != "" {
		fmt.Printf("   provider: %s (pinned)\n", *provider)
		return llmgate_adapter.New(gw, llmgate_adapter.Config{Provider: *provider})
	}
	fmt.Println("   routing: using config strategy")
	return llmgate_adapter.NewWithStrategy(gw)
}

func hasLLMKeyEnv() bool {
	for _, v := range []string{
		"DEEPSEEK_KEY", "OPENAI_KEY", "GLM_KEY", "ANTHROPIC_KEY",
		"QWEN_KEY", "KIMI_KEY", "DOUBAO_KEY", "GEMINI_KEY",
		"GROK_KEY", "MINIMAX_KEY", "STEPFUN_KEY",
	} {
		if os.Getenv(v) != "" {
			return true
		}
	}
	return false
}

// ── ReAct hook ────────────────────────────────────────────────────────────────

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
	fmt.Printf("  -> [%s] ...\n", nodeName)
}
func (h *reactHook) OnNodeEnd(_ context.Context, nodeName string, s *agent.MessageState, err error, dur time.Duration) {
	if err != nil {
		fmt.Printf("  x [%s] error: %v (%s)\n", nodeName, err, dur)
		return
	}
	if len(s.Messages) == 0 {
		return
	}
	last := s.Messages[len(s.Messages)-1]
	switch nodeName {
	case "llm":
		if len(last.ToolCalls) > 0 {
			calls := make([]string, len(last.ToolCalls))
			for i, tc := range last.ToolCalls {
				calls[i] = fmt.Sprintf("%s(%v)", tc.Name, tc.Arguments)
			}
			fmt.Printf("  [thought] -> Action: %s  (%s)\n", strings.Join(calls, ", "), dur)
		} else {
			fmt.Printf("  [thought] -> Answer: %s  (%s)\n", last.Content, dur)
		}
	case "tool":
		for _, msg := range s.Messages {
			if msg.Role == agent.RoleTool {
				fmt.Printf("  [observation] %s: %s\n", msg.ToolName, msg.Content)
			}
		}
	}
}
func (h *reactHook) OnRetry(_ context.Context, nodeName string, attempt int, lastErr error) {
	fmt.Printf("  retry [%s] %d: %v\n", nodeName, attempt, lastErr)
}

// ── Mock LLM ──────────────────────────────────────────────────────────────────

type mockLLM struct{}

func (m *mockLLM) Chat(_ context.Context, req *agent.ChatRequest) (*agent.ChatResponse, error) {
	for _, msg := range req.Messages {
		if msg.Role == agent.RoleTool {
			return &agent.ChatResponse{Content: "The answer is 56088.", FinishReason: "stop"}, nil
		}
	}
	return &agent.ChatResponse{
		ToolCalls:    []agent.ToolCall{{ID: "c1", Name: "calculator", Arguments: map[string]any{"expression": "123 * 456"}}},
		FinishReason: "tool_calls",
	}, nil
}

func (m *mockLLM) ChatStream(_ context.Context, req *agent.ChatRequest) (<-chan *agent.StreamChunk, error) {
	resp, err := m.Chat(context.Background(), req)
	if err != nil {
		return nil, err
	}
	ch := make(chan *agent.StreamChunk, 1)
	ch <- &agent.StreamChunk{
		Content:      resp.Content,
		ToolCalls:    resp.ToolCalls,
		FinishReason: resp.FinishReason,
		Usage:        resp.Usage,
	}
	close(ch)
	return ch, nil
}

func init() {
	os.Setenv("LLMGATE_QUIET", "1")
}
