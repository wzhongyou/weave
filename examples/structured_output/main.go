// structured_output 展示结构化输出 — 用 JSON Schema 约束 LLM 输出。
//
//  用法:
//    go run ./examples/structured_output
//
// Mock 模式运行，无需 API key。
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wzhongyou/graphflow/agent"
	"github.com/wzhongyou/graphflow/graph"
)

func main() {
	ctx := context.Background()

	// ── 1. 定义 JSON Schema ──────────────────────────────────────────────
	personSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "姓名",
			},
			"age": map[string]any{
				"type":        "integer",
				"description": "年龄",
			},
			"email": map[string]any{
				"type":        "string",
				"description": "邮箱地址",
			},
		},
		"required": []string{"name", "age"},
	}

	// ── 2. 创建支持结构化输出的 LLMNode ─────────────────────────────────
	llmNode := agent.NewLLMNode(agent.LLMNodeConfig{
		Model:  &jsonMockLLM{},
		Stream: false,
		StructuredOutput: &agent.StructuredOutputConfig{
			Schema:     personSchema,
			SchemaName: "人员信息",
		},
	})

	g := graph.NewGraph[*agent.MessageState]("structured-output-demo")
	g.AddNode("llm", llmNode.Run)
	g.SetEntryPoint("llm")
	g.Compile()

	engine := graph.NewEngine(g)

	// ── 3. 运行——合法输出 ───────────────────────────────────────────────
	fmt.Println("=== 测试 1: 合法 JSON 输出 ===")
	state := &agent.MessageState{
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "提取人员信息"}},
	}
	_, err := engine.Run(ctx, state)
	if err != nil {
		fmt.Printf("✗ 校验失败: %v\n", err)
	} else {
		last := state.Messages[len(state.Messages)-1]
		fmt.Printf("✓ 输出通过校验\n%s\n", prettifyJSON(last.Content))
	}

	// ── 4. 测试 2: 非法输出（缺少必需字段）─────────────────────────────
	fmt.Println("\n=== 测试 2: 缺少必需字段（应报错）===")
	llmNode2 := agent.NewLLMNode(agent.LLMNodeConfig{
		Model:  &invalidJSONMockLLM{},
		Stream: false,
		StructuredOutput: &agent.StructuredOutputConfig{
			Schema:     personSchema,
			SchemaName: "人员信息",
		},
	})
	g2 := graph.NewGraph[*agent.MessageState]("structured-output-demo-2")
	g2.AddNode("llm", llmNode2.Run)
	g2.SetEntryPoint("llm")
	g2.Compile()

	state2 := &agent.MessageState{
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "提取人员信息"}},
	}
	_, err2 := engine.Run(ctx, state2) // 注意：使用的是 g2 的 engine
	if err2 != nil {
		fmt.Printf("✓ 按预期捕获错误: %v\n", err2)
	} else {
		// 用正确的 engine
		engine2 := graph.NewEngine(g2)
		_, err2 = engine2.Run(ctx, state2)
		if err2 != nil {
			fmt.Printf("✓ 按预期捕获错误: %v\n", err2)
		}
	}
}

func prettifyJSON(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// ── Mock LLM: 返回合法 JSON ────────────────────────────────────────────────

type jsonMockLLM struct{}

func (m *jsonMockLLM) Chat(_ context.Context, req *agent.ChatRequest) (*agent.ChatResponse, error) {
	// 模拟 LLM 返回符合 Schema 的 JSON
	return &agent.ChatResponse{
		Content:      `{"name": "张三", "age": 28, "email": "zhangsan@example.com"}`,
		FinishReason: "stop",
	}, nil
}

func (m *jsonMockLLM) ChatStream(_ context.Context, req *agent.ChatRequest) (<-chan *agent.StreamChunk, error) {
	resp, err := m.Chat(context.Background(), req)
	if err != nil {
		return nil, err
	}
	ch := make(chan *agent.StreamChunk, 1)
	ch <- &agent.StreamChunk{Content: resp.Content, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

// ── Mock LLM: 返回缺少必需字段的 JSON ──────────────────────────────────────

type invalidJSONMockLLM struct{}

func (m *invalidJSONMockLLM) Chat(_ context.Context, _ *agent.ChatRequest) (*agent.ChatResponse, error) {
	// 缺少必需的 "name" 字段
	return &agent.ChatResponse{
		Content:      `{"age": 28}`,
		FinishReason: "stop",
	}, nil
}

func (m *invalidJSONMockLLM) ChatStream(_ context.Context, req *agent.ChatRequest) (<-chan *agent.StreamChunk, error) {
	resp, err := m.Chat(context.Background(), req)
	if err != nil {
		return nil, err
	}
	ch := make(chan *agent.StreamChunk, 1)
	ch <- &agent.StreamChunk{Content: resp.Content, FinishReason: "stop"}
	close(ch)
	return ch, nil
}
