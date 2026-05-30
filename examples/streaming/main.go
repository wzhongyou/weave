// streaming_demo 展示 Graphflow 的流式响应能力。
//
//  用法:
//    go run ./examples/streaming
//
// 演示内容:
//   1. LLMNode.OnChunk — 逐 Token 打印 LLM 输出
//   2. Engine.RunStream — 流式图执行事件（节点开始/结束）
//
// Mock 模式运行，无需 API key。
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wzhongyou/graphflow/agent"
	"github.com/wzhongyou/graphflow/graph"
)

func main() {
	ctx := context.Background()

	// ── 1. 创建支持流式的 LLMNode ────────────────────────────────────────
	llmNode := agent.NewLLMNode(agent.LLMNodeConfig{
		Model:  &streamingMockLLM{},
		Stream: true,
		OnChunk: func(chunk *agent.StreamChunk) {
			if chunk.Content != "" {
				fmt.Print(chunk.Content)
			}
		},
	})

	// ── 2. 构建图 ──────────────────────────────────────────────────────────
	g := graph.NewGraph[*agent.MessageState]("streaming-demo")
	g.AddNode("llm", llmNode.Run)
	g.SetEntryPoint("llm")
	g.Compile()

	engine := graph.NewEngine(g)

	// ── 3. 方式一：OnChunk 实时 Token 输出 ────────────────────────────────
	fmt.Println("=== OnChunk: 逐 Token 流式输出 ===")
	state := &agent.MessageState{
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "用中文介绍自己"}},
	}
	_, err := engine.Run(ctx, state)
	if err != nil {
		panic(err)
	}
	fmt.Println("\n✓ 流式输出完成")
	fmt.Printf("总 Token: %d\n\n", state.TotalTokens)

	// ── 4. 方式二：RunStream 流式执行事件 ─────────────────────────────────
	fmt.Println("=== RunStream: 图执行事件流 ===")
	state2 := &agent.MessageState{
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "你好"}},
	}

	stream, err := engine.RunStream(ctx, state2)
	if err != nil {
		panic(err)
	}

	for event := range stream.Chan() {
		switch event.Type {
		case graph.StreamNodeStart:
			fmt.Printf("▶ 节点开始: %s\n", event.NodeName)
		case graph.StreamNodeEnd:
			fmt.Printf("■ 节点结束: %s (%v)\n", event.NodeName, event.Duration)
		case graph.StreamGraphEnd:
			fmt.Println("◆ 图执行完成")
		}
	}
}

// ── Mock 流式 LLM ──────────────────────────────────────────────────────────

type streamingMockLLM struct{}

func (m *streamingMockLLM) Chat(_ context.Context, req *agent.ChatRequest) (*agent.ChatResponse, error) {
	return &agent.ChatResponse{
		Content:      "这是一条流式响应。每个词都会通过 OnChunk 回调实时输出。",
		FinishReason: "stop",
	}, nil
}

func (m *streamingMockLLM) ChatStream(_ context.Context, _ *agent.ChatRequest) (<-chan *agent.StreamChunk, error) {
	ch := make(chan *agent.StreamChunk, 20)
	go func() {
		words := strings.Fields("这是一条流式响应。每个词都会通过 OnChunk 回调实时输出。")
		for _, word := range words {
			ch <- &agent.StreamChunk{Content: word + " "}
			time.Sleep(100 * time.Millisecond) // 模拟逐个 Token 生成
		}
		ch <- &agent.StreamChunk{
			Content:      "",
			FinishReason: "stop",
			Usage:        &agent.Usage{InputTokens: 10, OutputTokens: 12, TotalTokens: 22},
		}
		close(ch)
	}()
	return ch, nil
}
