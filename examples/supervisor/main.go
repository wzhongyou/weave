// supervisor_demo 展示多智能体编排 — SupervisorAgent 路由到子智能体。
//
//  用法:
//    go run ./examples/supervisor
//
// 架构: supervisor ─[route]─→ sub-agent ─→ collect ─→ supervisor (循环)
//
// Mock 模式运行，无需 API key。
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wzhongyou/graphflow/agent"
	"github.com/wzhongyou/graphflow/graph"
)

func main() {
	ctx := context.Background()

	// ── 1. 定义子智能体 ──────────────────────────────────────────────────
	// "calculator" 子智能体：使用 CalculatorTool 做数学计算
	calculatorLLM := &roundRobinMockLLM{
		responses: []mockResponse{
			{content: "", toolName: "calculator", toolArgs: map[string]any{"expression": "123 * 456"}},
			{content: "计算结果: 123 * 456 = 56088"},
		},
	}
	calculatorAgent := agent.NewReActAgent(agent.ReActAgentConfig{
		Name:         "calculator",
		LLM:          calculatorLLM,
		SystemPrompt: "你是计算器智能体。用 calculator 工具计算数学表达式。",
		Tools:        []agent.Tool{&agent.CalculatorTool{}},
		MaxSteps:     5,
	})

	// "echo" 子智能体：简单的文字处理
	echoLLM := &roundRobinMockLLM{
		responses: []mockResponse{
			{content: "你说了: Hello World，我已收到！"},
		},
	}
	echoAgent := agent.NewReActAgent(agent.ReActAgentConfig{
		Name:         "echo",
		LLM:          echoLLM,
		SystemPrompt: "你是复读机智能体。直接回复用户的消息。",
		MaxSteps:     3,
	})

	// ── 2. 创建管理者智能体 ─────────────────────────────────────────────
	// 模拟管理者 LLM 的决策过程：
	//   - 第1步：路由到 "calculator"
	//   - 第2步：路由到 "echo"（如果 NextAgent 为空则直接结束）
	supervisorLLM := &roundRobinMockLLM{
		responses: []mockResponse{
			{content: "", toolName: "route", toolArgs: map[string]any{"agent": "calculator"}},
			{content: "", toolName: "route", toolArgs: map[string]any{"agent": "echo"}},
			{content: "所有子智能体已完成任务。"},
		},
	}

	supAgent := agent.NewSupervisorAgent(agent.SupervisorAgentConfig{
		Name: "supervisor-demo",
		LLM:  supervisorLLM,
		SubAgents: map[string]agent.SubAgent{
			"calculator": calculatorAgent,
			"echo":       echoAgent,
		},
		MaxRounds: 10,
	})

	g, err := supAgent.BuildGraph()
	if err != nil {
		panic(err)
	}

	// ── 3. 运行 ─────────────────────────────────────────────────────────
	engine := graph.NewEngine(g)
	state := &agent.MessageState{
		Messages: []agent.Message{{
			Role:    agent.RoleUser,
			Content: "帮我计算 123*456，然后回复 Hello World",
		}},
	}

	fmt.Println("=== 多智能体编排演示 ===")
	fmt.Printf("问题: %s\n\n", state.Messages[0].Content)

	result, err := engine.Run(ctx, state, graph.WithHook(&traceHook{}))
	if err != nil {
		panic(err)
	}

	fmt.Println("\n=== 执行结果 ===")
	fmt.Printf("完成智能体: %v\n", result.FinalState.CompletedAgents)
	if len(result.FinalState.Messages) > 0 {
		last := result.FinalState.Messages[len(result.FinalState.Messages)-1]
		fmt.Printf("最终输出: %s\n", last.Content)
	}
	fmt.Printf("执行步数: %d\n", result.TotalSteps)
	fmt.Printf("总耗时: %v\n", result.TotalDuration.Round(time.Millisecond))
}

// ── Mock LLM ────────────────────────────────────────────────────────────────

type mockResponse struct {
	content  string
	toolName string
	toolArgs map[string]any
}

// roundRobinMockLLM 按顺序返回预定义的响应序列。
type roundRobinMockLLM struct {
	responses []mockResponse
	index     int
}

func (m *roundRobinMockLLM) Chat(_ context.Context, _ *agent.ChatRequest) (*agent.ChatResponse, error) {
	if m.index >= len(m.responses) {
		m.index = len(m.responses) - 1
	}
	resp := m.responses[m.index]
	m.index++

	cr := &agent.ChatResponse{
		Content:      resp.content,
		FinishReason: "stop",
	}
	if resp.toolName != "" {
		cr.ToolCalls = []agent.ToolCall{
			{ID: fmt.Sprintf("call-%d", m.index), Name: resp.toolName, Arguments: resp.toolArgs},
		}
		cr.FinishReason = "tool_calls"
	}
	return cr, nil
}

func (m *roundRobinMockLLM) ChatStream(ctx context.Context, req *agent.ChatRequest) (<-chan *agent.StreamChunk, error) {
	resp, err := m.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan *agent.StreamChunk, 1)
	ch <- &agent.StreamChunk{
		Content:      resp.Content,
		ToolCalls:    resp.ToolCalls,
		FinishReason: resp.FinishReason,
	}
	close(ch)
	return ch, nil
}

// ── Hook 追踪 ──────────────────────────────────────────────────────────────

type traceHook struct{}

func (h *traceHook) OnNodeStart(_ context.Context, name string, s *agent.MessageState) {
	fmt.Printf("  ▶ [%s]\n", name)
}
func (h *traceHook) OnNodeEnd(_ context.Context, name string, s *agent.MessageState, err error, dur time.Duration) {
	if err != nil {
		fmt.Printf("  ✗ [%s] 错误: %v (%v)\n", name, err, dur)
		return
	}
	if len(s.Messages) > 0 {
		last := s.Messages[len(s.Messages)-1]
		if last.Role == agent.RoleTool {
			fmt.Printf("  ✓ [%s] → 工具结果: %s (%v)\n", name, last.Content, dur)
		} else if len(last.ToolCalls) > 0 {
			fmt.Printf("  ✓ [%s] → 调用: %s (%v)\n", name, last.ToolCalls[0].Name, dur)
		} else if last.Content != "" {
			fmt.Printf("  ✓ [%s] → %s (%v)\n", name, last.Content, dur)
		}
	}
}
func (h *traceHook) OnGraphStart(_ context.Context, name string, _ *agent.MessageState) {
	fmt.Printf("◆ 图开始: %s\n", name)
}
func (h *traceHook) OnGraphEnd(_ context.Context, _ string, _ *agent.MessageState, err error) {
	if err != nil {
		fmt.Printf("◆ 图失败: %v\n", err)
	}
}
func (h *traceHook) OnRetry(_ context.Context, _ string, _ int, _ error) {}
