package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/wzhongyou/graphflow/graph"
)

// ── test helpers ───────────────────────────────────────────────────────────────

type testLLM struct {
	responses map[string]*ChatResponse // keyed by session trace
	calls     []*ChatRequest
}

func (m *testLLM) Chat(_ context.Context, req *ChatRequest) (*ChatResponse, error) {
	m.calls = append(m.calls, req)
	// If the last message is a tool result, return the final answer.
	if len(req.Messages) > 0 {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == RoleTool {
			return &ChatResponse{Content: "The answer is 42.", FinishReason: "stop"}, nil
		}
	}
	// Otherwise, request a tool call.
	return &ChatResponse{
		ToolCalls: []ToolCall{
			{ID: "tc1", Name: "test_tool", Arguments: map[string]any{"a": "1"}},
		},
		FinishReason: "tool_calls",
	}, nil
}

func (m *testLLM) ChatStream(_ context.Context, req *ChatRequest) (<-chan *StreamChunk, error) {
	m.calls = append(m.calls, req)
	ch := make(chan *StreamChunk, 2)
	if len(req.Messages) > 0 {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == RoleTool {
			ch <- &StreamChunk{Content: "The answer is 42.", FinishReason: "stop"}
			close(ch)
			return ch, nil
		}
	}
	ch <- &StreamChunk{
		ToolCalls: []ToolCall{
			{ID: "tc1", Name: "test_tool", Arguments: map[string]any{"a": "1"}},
		},
		FinishReason: "tool_calls",
	}
	close(ch)
	return ch, nil
}

type testTool struct {
	name        string
	description string
	params      map[string]any
	execute     func(ctx context.Context, args map[string]any) (string, error)
}

func (t *testTool) Name() string             { return t.name }
func (t *testTool) Description() string       { return t.description }
func (t *testTool) Parameters() map[string]any { return t.params }
func (t *testTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return t.execute(ctx, args)
}

type testEmbedder struct {
	embed func(ctx context.Context, text string) ([]float32, error)
}

func (e *testEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return e.embed(ctx, text)
}

type testVectorStore struct {
	search func(ctx context.Context, query []float32, topK int) ([]SearchResult, error)
	insert func(ctx context.Context, id string, vector []float32, metadata map[string]any) error
}

func (s *testVectorStore) Insert(ctx context.Context, id string, v []float32, m map[string]any) error {
	return s.insert(ctx, id, v, m)
}
func (s *testVectorStore) Search(ctx context.Context, q []float32, k int) ([]SearchResult, error) {
	return s.search(ctx, q, k)
}

// ── LLMNode tests ──────────────────────────────────────────────────────────────

func TestLLMNode_PlainChat(t *testing.T) {
	responder := &testLLM{
		responses: map[string]*ChatResponse{
			"*": {Content: "Hello!", FinishReason: "stop"},
		},
	}
	// Override the default behaviour.
	responder.calls = nil
	called := false
	llm := &testLLM{
		responses: nil,
		calls:     nil,
	}
	// We'll use a closure-based mock.
	state := &MessageState{
		Messages: []Message{
			{Role: RoleUser, Content: "hi"},
		},
	}
	_ = llm
	_ = called

	// Use a simple inline mock that implements LLMModel via struct.
	node := NewLLMNode(LLMNodeConfig{
		Model: &plainChatLLM{},
	})
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(next.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(next.Messages))
	}
	last := next.Messages[len(next.Messages)-1]
	if last.Role != RoleAssistant {
		t.Fatalf("expected assistant role, got %s", last.Role)
	}
	if last.Content != "hi back" {
		t.Fatalf("expected 'hi back', got %q", last.Content)
	}
	if next.StepCount != 1 {
		t.Fatalf("expected StepCount=1, got %d", next.StepCount)
	}
}

type plainChatLLM struct{}

func (m *plainChatLLM) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Content: "hi back", FinishReason: "stop"}, nil
}
func (m *plainChatLLM) ChatStream(_ context.Context, _ *ChatRequest) (<-chan *StreamChunk, error) {
	ch := make(chan *StreamChunk, 1)
	ch <- &StreamChunk{Content: "hi back", FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func TestLLMNode_ToolCallResponse(t *testing.T) {
	node := NewLLMNode(LLMNodeConfig{
		Model: &toolCallLLM{},
		Tools: []Tool{
			&testTool{name: "adder", description: "adds two numbers", params: map[string]any{}},
		},
	})
	state := &MessageState{
		Messages: []Message{{Role: RoleUser, Content: "what is 1+2?"}},
	}
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := next.Messages[len(next.Messages)-1]
	if last.Role != RoleAssistant {
		t.Fatalf("expected assistant role, got %s", last.Role)
	}
	if len(last.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(last.ToolCalls))
	}
	if last.ToolCalls[0].Name != "adder" {
		t.Fatalf("expected 'adder' tool call, got %s", last.ToolCalls[0].Name)
	}
}

type toolCallLLM struct{}

func (m *toolCallLLM) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{
		ToolCalls:    []ToolCall{{ID: "1", Name: "adder", Arguments: map[string]any{"a": 1, "b": 2}}},
		FinishReason: "tool_calls",
	}, nil
}
func (m *toolCallLLM) ChatStream(_ context.Context, _ *ChatRequest) (<-chan *StreamChunk, error) {
	ch := make(chan *StreamChunk, 1)
	ch <- &StreamChunk{
		ToolCalls:    []ToolCall{{ID: "1", Name: "adder", Arguments: map[string]any{"a": 1, "b": 2}}},
		FinishReason: "tool_calls",
	}
	close(ch)
	return ch, nil
}

func TestLLMNode_SystemPromptPrepended(t *testing.T) {
	// Use a model that records the received messages so we can verify
	// the system prompt was injected into the request.
	var capturedReq *ChatRequest
	node := NewLLMNode(LLMNodeConfig{
		Model: &captureLLM{fn: func(req *ChatRequest) *ChatResponse {
			capturedReq = req
			return &ChatResponse{Content: "ok", FinishReason: "stop"}
		}},
		SystemPrompt: "You are helpful.",
	})
	state := &MessageState{
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}
	_, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedReq == nil {
		t.Fatal("expected model to be called")
	}
	if len(capturedReq.Messages) != 2 {
		t.Fatalf("expected 2 messages in request (system + user), got %d", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != RoleSystem {
		t.Fatalf("expected system message first in request, got %s", capturedReq.Messages[0].Role)
	}
	if capturedReq.Messages[0].Content != "You are helpful." {
		t.Fatalf("expected system prompt, got %q", capturedReq.Messages[0].Content)
	}
}

type captureLLM struct {
	fn func(req *ChatRequest) *ChatResponse
}

func (m *captureLLM) Chat(_ context.Context, req *ChatRequest) (*ChatResponse, error) {
	return m.fn(req), nil
}
func (m *captureLLM) ChatStream(_ context.Context, req *ChatRequest) (<-chan *StreamChunk, error) {
	resp := m.fn(req)
	ch := make(chan *StreamChunk, 1)
	ch <- &StreamChunk{
		Content:      resp.Content,
		ToolCalls:    resp.ToolCalls,
		FinishReason: resp.FinishReason,
		Usage:        resp.Usage,
	}
	close(ch)
	return ch, nil
}

func TestLLMNode_SystemPromptNotDuplicated(t *testing.T) {
	node := NewLLMNode(LLMNodeConfig{
		Model:        &plainChatLLM{},
		SystemPrompt: "You are helpful.",
	})
	state := &MessageState{
		Messages: []Message{
			{Role: RoleSystem, Content: "Existing system prompt"},
			{Role: RoleUser, Content: "hello"},
		},
	}
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(next.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(next.Messages))
	}
	if next.Messages[0].Content != "Existing system prompt" {
		t.Fatalf("expected existing system prompt to be preserved, got %q", next.Messages[0].Content)
	}
}

// ── ToolNode tests ─────────────────────────────────────────────────────────────

func TestToolNode_ExecuteToolCall(t *testing.T) {
	tool := &testTool{
		name:        "echo",
		description: "echoes back",
		params:      map[string]any{},
		execute: func(_ context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("echo: %v", args["msg"]), nil
		},
	}
	node := NewToolNode(tool)
	state := &MessageState{
		Messages: []Message{
			{Role: RoleUser, Content: "hi"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "1", Name: "echo", Arguments: map[string]any{"msg": "hello"}},
			}},
		},
	}
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(next.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(next.Messages))
	}
	toolMsg := next.Messages[2]
	if toolMsg.Role != RoleTool {
		t.Fatalf("expected tool role, got %s", toolMsg.Role)
	}
	if toolMsg.Content != "echo: hello" {
		t.Fatalf("expected 'echo: hello', got %q", toolMsg.Content)
	}
}

func TestToolNode_ToolNotFound(t *testing.T) {
	node := NewToolNode() // empty registry
	state := &MessageState{
		Messages: []Message{
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "1", Name: "nonexistent", Arguments: nil},
			}},
		},
	}
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	toolMsg := next.Messages[1]
	if toolMsg.Role != RoleTool {
		t.Fatalf("expected tool role, got %s", toolMsg.Role)
	}
	if toolMsg.Content != `error: tool "nonexistent" not found` {
		t.Fatalf("expected tool not found error, got %q", toolMsg.Content)
	}
}

func TestToolNode_NoPendingToolCalls(t *testing.T) {
	tool := &testTool{name: "t1"}
	node := NewToolNode(tool)
	state := &MessageState{
		Messages: []Message{
			{Role: RoleAssistant, Content: "done"},
		},
	}
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(next.Messages) != 1 {
		t.Fatalf("expected no new messages, got %d", len(next.Messages))
	}
}

func TestToolNode_EmptyMessages(t *testing.T) {
	node := NewToolNode(&testTool{name: "t1"})
	state := &MessageState{}
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(next.Messages) != 0 {
		t.Fatalf("expected no messages, got %d", len(next.Messages))
	}
}

func TestToolNode_ToolExecutionError(t *testing.T) {
	tool := &testTool{
		name: "faulty",
		execute: func(_ context.Context, _ map[string]any) (string, error) {
			return "", errors.New("boom")
		},
	}
	node := NewToolNode(tool)
	state := &MessageState{
		Messages: []Message{
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "1", Name: "faulty", Arguments: nil},
			}},
		},
	}
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	toolMsg := next.Messages[1]
	if toolMsg.Content != "error: boom" {
		t.Fatalf("expected 'error: boom', got %q", toolMsg.Content)
	}
}

// ── ReActAgent tests ───────────────────────────────────────────────────────────

func TestReActAgent_BuildGraph(t *testing.T) {
	ag := NewReActAgent(ReActAgentConfig{
		LLM:      &plainChatLLM{},
		Tools:    []Tool{&testTool{name: "t1"}},
		MaxSteps: 10,
	})
	g, err := ag.BuildGraph()
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
}

func TestReActAgent_FullLoop(t *testing.T) {
	// Use testLLM which first returns tool_calls, then final answer.
	ag := NewReActAgent(ReActAgentConfig{
		LLM:      &testLLM{},
		Tools:    []Tool{&testTool{name: "test_tool", execute: func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil }}},
		MaxSteps: 10,
	})
	g, err := ag.BuildGraph()
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}

	engine := graph.NewEngine(g)
	result, err := engine.Run(context.Background(), &MessageState{
		Messages: []Message{{Role: RoleUser, Content: "question?"}},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	msgs := result.FinalState.Messages
	// Should have: user, assistant(tool_calls), tool(result), assistant(answer)
	if len(msgs) < 4 {
		t.Fatalf("expected at least 4 messages, got %d", len(msgs))
	}
	// Last message should be the final answer.
	last := msgs[len(msgs)-1]
	if last.Role != RoleAssistant || last.Content != "The answer is 42." {
		t.Fatalf("expected final answer, got role=%s content=%q", last.Role, last.Content)
	}
}

func TestReActAgent_HasPendingToolCalls(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		want     bool
	}{
		{"empty", nil, false},
		{"assistant_no_tool_calls", []Message{{Role: RoleAssistant, Content: "hi"}}, false},
		{"user_message", []Message{{Role: RoleUser, Content: "?"}}, false},
		{"pending_tool_calls", []Message{{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "1"}}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &MessageState{Messages: tt.messages}
			got := HasPendingToolCalls(context.Background(), s)
			if got != tt.want {
				t.Fatalf("HasPendingToolCalls = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── RAGAgent tests ─────────────────────────────────────────────────────────────

func TestRAGAgent_BuildGraph(t *testing.T) {
	emb := &testEmbedder{embed: func(_ context.Context, s string) ([]float32, error) {
		return []float32{0.1, 0.2}, nil
	}}
	store := &testVectorStore{
		search: func(_ context.Context, q []float32, k int) ([]SearchResult, error) {
			return []SearchResult{{ID: "doc1", Score: 0.9}}, nil
		},
	}
	ag := NewRAGAgent(RAGAgentConfig{
		LLM:         &plainChatLLM{},
		Embedder:    emb,
		VectorStore: store,
		TopK:        3,
	})
	g, err := ag.BuildGraph()
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
}

func TestRAGAgent_RetrieveThenGenerate(t *testing.T) {
	emb := &testEmbedder{embed: func(_ context.Context, s string) ([]float32, error) {
		return []float32{0.1}, nil
	}}
	store := &testVectorStore{
		search: func(_ context.Context, q []float32, k int) ([]SearchResult, error) {
			return []SearchResult{{ID: "doc1", Score: 0.95, Metadata: map[string]any{"text": "relevant context"}}, {ID: "doc2", Score: 0.8}}, nil
		},
	}
	ag := NewRAGAgent(RAGAgentConfig{
		LLM:         &plainChatLLM{},
		Embedder:    emb,
		VectorStore: store,
		TopK:        2,
	})
	g, err := ag.BuildGraph()
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}

	engine := graph.NewEngine(g)
	result, err := engine.Run(context.Background(), &MessageState{
		Messages: []Message{{Role: RoleUser, Content: "query"}},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	ctx := result.FinalState.Context
	if ctx == nil {
		t.Fatal("expected context to be set")
	}
	docs, ok := ctx["retrieved_docs"].([]SearchResult)
	if !ok || len(docs) != 2 {
		t.Fatalf("expected 2 retrieved docs in context, got %v", ctx["retrieved_docs"])
	}
	// Check that the LLM also appended a response.
	msgs := result.FinalState.Messages
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
}

// ── ShortTermMemory tests ──────────────────────────────────────────────────────

func TestShortTermMemory_Add(t *testing.T) {
	m := NewShortTermMemory(3)
	m.Add(Message{Role: RoleUser, Content: "1"})
	m.Add(Message{Role: RoleAssistant, Content: "2"})
	m.Add(Message{Role: RoleUser, Content: "3"})
	if len(m.Messages()) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(m.Messages()))
	}
}

func TestShortTermMemory_Eviction(t *testing.T) {
	m := NewShortTermMemory(3)
	m.Add(Message{Role: RoleUser, Content: "1"})
	m.Add(Message{Role: RoleAssistant, Content: "2"})
	m.Add(Message{Role: RoleUser, Content: "3"})
	m.Add(Message{Role: RoleAssistant, Content: "4"})
	if len(m.Messages()) != 3 {
		t.Fatalf("expected 3 messages after eviction, got %d", len(m.Messages()))
	}
	if m.Messages()[0].Content != "2" {
		t.Fatalf("expected oldest non-system message to be evicted, got %q", m.Messages()[0].Content)
	}
}

func TestShortTermMemory_PreservesSystemMessage(t *testing.T) {
	m := NewShortTermMemory(3)
	m.Add(Message{Role: RoleSystem, Content: "system"})
	m.Add(Message{Role: RoleUser, Content: "1"})
	m.Add(Message{Role: RoleAssistant, Content: "2"})
	m.Add(Message{Role: RoleUser, Content: "3"})
	if len(m.Messages()) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(m.Messages()))
	}
	if m.Messages()[0].Role != RoleSystem {
		t.Fatalf("expected system message preserved at front, got %s", m.Messages()[0].Role)
	}
}

// ── CalculatorTool tests ───────────────────────────────────────────────────────

func TestCalculatorTool_Simple(t *testing.T) {
	tool := &CalculatorTool{}
	result, err := tool.Execute(context.Background(), map[string]any{"expression": "2 + 3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "5" {
		t.Fatalf("expected '5', got %q", result)
	}
}

func TestCalculatorTool_Complex(t *testing.T) {
	tool := &CalculatorTool{}
	result, err := tool.Execute(context.Background(), map[string]any{"expression": "(3 + 4) * 5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := strconv.FormatFloat((3.0+4.0)*5.0, 'f', -1, 64)
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestCalculatorTool_DivisionByZero(t *testing.T) {
	tool := &CalculatorTool{}
	_, err := tool.Execute(context.Background(), map[string]any{"expression": "1 / 0"})
	if err == nil {
		t.Fatal("expected error for division by zero")
	}
}

func TestCalculatorTool_MissingArgument(t *testing.T) {
	tool := &CalculatorTool{}
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

// ── ToolRegistry tests ─────────────────────────────────────────────────────────

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	tool := &testTool{name: "echo"}
	r.Register(tool)
	got, ok := r.Get("echo")
	if !ok {
		t.Fatal("expected tool 'echo' to be found")
	}
	if got.Name() != "echo" {
		t.Fatalf("expected 'echo', got %q", got.Name())
	}
}

func TestToolRegistry_GetMissing(t *testing.T) {
	r := NewToolRegistry()
	_, ok := r.Get("missing")
	if ok {
		t.Fatal("expected missing tool to return false")
	}
}

func TestToolRegistry_List(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&testTool{name: "a"})
	r.Register(&testTool{name: "b"})
	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(list))
	}
}

func TestToolRegistry_Len(t *testing.T) {
	r := NewToolRegistry()
	if r.Len() != 0 {
		t.Fatal("expected 0")
	}
	r.Register(&testTool{name: "a"})
	if r.Len() != 1 {
		t.Fatal("expected 1")
	}
}

// ── ToolDefs helper test ───────────────────────────────────────────────────────

func TestToolDefs(t *testing.T) {
	tools := []Tool{
		&testTool{name: "alpha", description: "first tool", params: map[string]any{"type": "object"}},
		&testTool{name: "beta", description: "second tool", params: nil},
	}
	defs := ToolDefs(tools)
	if len(defs) != 2 {
		t.Fatalf("expected 2 ToolDefs, got %d", len(defs))
	}
	if defs[0].Name != "alpha" || defs[0].Description != "first tool" {
		t.Fatalf("unexpected def[0]: %+v", defs[0])
	}
	if defs[1].Name != "beta" {
		t.Fatalf("unexpected def[1].Name: %s", defs[1].Name)
	}
}

// ── LongTermMemory tests ───────────────────────────────────────────────────────

func TestLongTermMemory_RememberNilDeps(t *testing.T) {
	m := NewLongTermMemory(nil, nil)
	err := m.Remember(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error when embedder/vectorStore are nil")
	}
}

func TestLongTermMemory_RecallNilDeps(t *testing.T) {
	m := NewLongTermMemory(nil, nil)
	_, err := m.Recall(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error when embedder/vectorStore are nil")
	}
}

// ── VectorRetrieveNode tests ───────────────────────────────────────────────────

func TestVectorRetrieveNode_NoMessages(t *testing.T) {
	node := &VectorRetrieveNode{
		Embedder:    &testEmbedder{},
		VectorStore: &testVectorStore{},
		TopK:        5,
	}
	state := &MessageState{}
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(next.Messages) != 0 {
		t.Fatalf("expected no messages added, got %d", len(next.Messages))
	}
}

func TestVectorRetrieveNode_NilDeps(t *testing.T) {
	node := &VectorRetrieveNode{TopK: 5}
	state := &MessageState{
		Messages: []Message{{Role: RoleUser, Content: "query"}},
	}
	next, err := node.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Context != nil {
		t.Fatal("expected context to remain nil when deps are nil")
	}
}
