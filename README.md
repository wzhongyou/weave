# Graphflow

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/wzhongyou/graphflow)](https://goreportcard.com/report/github.com/wzhongyou/graphflow)
[![Go Reference](https://pkg.go.dev/badge/github.com/wzhongyou/graphflow.svg)](https://pkg.go.dev/github.com/wzhongyou/graphflow)

**Graphflow** is a Go-native Agent development framework built on a type-safe graph execution engine.

> Mental model: **Graph = Nodes + Edges + State machine**. Nodes process state, edges control flow, the engine runs a Pregel-style superstep loop. It's as simple as writing plain Go functions.

> [中文文档](README_zh.md)

---

## Installation

```bash
go get github.com/wzhongyou/graphflow
```

Requires Go 1.21+. The core `graph/` engine has zero external dependencies.

---

## Run in 5 seconds

```bash
go run ./examples/agent_demo
```

No configuration needed — mock mode runs immediately:

```
[react-agent] question: What is 123 * 456?
  [Thought] → Action: calculator  (12ms)
  [Observation] calculator: 56088
  [Thought] → Answer: The answer is 56088.

[done] steps=3  duration=21ms
```

That's a ReAct Agent: LLM thinks → calls calculator → returns the result.

---

## When to use Graphflow

| Use case | Fit | Notes |
|----------|-----|-------|
| AI Agents (tool calling, RAG, multi-step reasoning) | **Best fit** | ReAct / RAG built-in |
| Business workflows (order processing, ETL) | Good fit | Pure graph engine, AI-agnostic |
| Non-Python deployment environments | **Best fit** | Single Go binary, no Python runtime |
| Production with resilience requirements | **Best fit** | Circuit breaker, retry, timeout, bulkhead built-in |
| Heavy Python ecosystem (LangChain, etc.) | Not a fit | Consider LangGraph |

---

## Why Graphflow

Most Agent frameworks are Python-first, bring heavy runtimes, and blur the line between orchestration and AI logic. Graphflow is different:

| | Graphflow | LangGraph | Eino |
|---|---|---|---|
| Language | **Go** | Python | Go |
| Core abstraction | **Graph engine** | StateGraph | Graph + Chain |
| No Python runtime | **Yes** | No | **Yes** |
| Zero-dependency core | **Yes** | No | No |
| Built-in resilience | **CB / Retry / Bulkhead / Rate limit** | Limited | Limited |

---

## Architecture

```
┌──────────────────────────────────────────┐
│  Your Application                        │
│  Business workflows · AI Agent apps      │
└─────────────────────┬────────────────────┘
                      │
┌─────────────────────▼────────────────────┐
│  agent/   — Agent Development Layer      │
│  MessageState · LLMNode · ToolNode       │
│  ReAct · RAG · Supervisor patterns       │
│  ToolRegistry · Memory management        │
└─────────────────────┬────────────────────┘
                      │ depends on
┌─────────────────────▼────────────────────┐
│  graph/   — Graph Execution Engine       │
│  Graph[S] · Engine[S] · NodeFunc[S]      │
│  Sequential · Conditional · Parallel     │
│  Loop · Checkpoint · Hook · OTel         │
│  middleware/ · node/ · checkpoint/       │
└──────────────────────────────────────────┘
```

The `graph/` package is **AI-agnostic** — it works equally well for business process orchestration, ETL pipelines, and event-driven workflows. The `agent/` package adds the AI-specific layer on top.

---

## Quick Start

### 1. Mock mode (zero config, run now)

```go
package main

import (
    "context"
    "fmt"
    "github.com/wzhongyou/graphflow/agent"
    "github.com/wzhongyou/graphflow/graph"
)

func main() {
    ag := agent.NewReActAgent(agent.ReActAgentConfig{
        Name:         "my-agent",
        SystemPrompt: "You are a helpful assistant.",
        Tools:        []agent.Tool{&agent.CalculatorTool{}},
    })
    g, err := ag.BuildGraph()
    if err != nil {
        panic(err)
    }

    engine := graph.NewEngine(g)
    result, err := engine.Run(context.Background(), &agent.MessageState{
        Messages: []agent.Message{{Role: agent.RoleUser, Content: "What is 100 + 200?"}},
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(result.FinalState.Messages[len(result.FinalState.Messages)-1].Content)
}
```

Omit the `LLM` field and it uses a built-in mock model (calculator only) — no API key needed.

### 2. Connect a real LLM

Graphflow uses [llmgate](https://github.com/wzhongyou/llmgate) as the LLM gateway (19 providers).

```bash
# Option 1: config file (recommended)
cp config/llmgate.toml.example config/llmgate.toml   # edit your API keys
go run ./examples/agent_demo -q "What is 100 + 200?"

# Option 2: environment variables
export DEEPSEEK_KEY="sk-xxx"
go run ./examples/agent_demo -env -provider deepseek -q "What is 100 + 200?"
```

Only 3 extra lines in code:

```go
import llmgate_adapter "github.com/wzhongyou/graphflow/agent/llmgate"
import "github.com/wzhongyou/llmgate/sdk"

gw, _ := sdk.NewFromFile("config/llmgate.toml")
adapter := llmgate_adapter.New(gw, llmgate_adapter.Config{Provider: "deepseek"})

ag := agent.NewReActAgent(agent.ReActAgentConfig{
    LLM: adapter,  // pass in the LLM
    // ...
})
```

### 3. Build the graph by hand (full control)

```go
llmNode   := agent.NewLLMNode(agent.LLMNodeConfig{Model: adapter, Tools: tools})
toolNode  := agent.NewToolNode(tools...)

g := graph.NewGraph[*agent.MessageState]("react-agent")
g.AddNode("llm", llmNode.Run)
g.AddNode("tool", toolNode.Run)
g.SetEntryPoint("llm")
g.AddCondition("llm", graph.Condition[*agent.MessageState]{
    If:     agent.HasPendingToolCalls,
    Target: "tool",
})
g.AddEdge("tool", "llm")   // loop back
g.SetMaxIterations("llm", 20)
g.Compile()
```

---

## Observing execution with Hooks

Implement the `graph.Hook` interface to observe every step:

```go
type reactHook struct{}

func (h *reactHook) OnNodeStart(_ context.Context, node string, s *agent.MessageState) {
    if node == "llm" {
        fmt.Printf("[Start] LLM thinking...\n")
    }
}

func (h *reactHook) OnNodeEnd(_ context.Context, node string, s *agent.MessageState, _ error, dur time.Duration) {
    last := s.Messages[len(s.Messages)-1]
    switch node {
    case "llm":
        if len(last.ToolCalls) > 0 {
            fmt.Printf("[Thought] → Action: %s  (%s)\n", last.ToolCalls[0].Name, dur)
        } else {
            fmt.Printf("[Thought] → Answer: %s\n", last.Content)
        }
    case "tool":
        for _, m := range s.Messages {
            if m.Role == agent.RoleTool {
                fmt.Printf("[Observation] %s: %s\n", m.ToolName, m.Content)
            }
        }
    }
}

// Usage
engine.Run(ctx, state, graph.WithHook(&reactHook{}))
```

Full Hook interface: `OnGraphStart` · `OnGraphEnd` · `OnNodeStart` · `OnNodeEnd` · `OnError`. Combine multiple hooks with `graph.ComposeHooks`.

---

## Resilience Middleware

Node functions are plain `func(ctx, state) (state, error)` — wrap them with middleware:

```go
// Recommended composition order (outer → inner)
node := middleware.WithRecover("payment",           // panic → error
    middleware.WithTimeout(chargePayment, 5*time.Second,
        middleware.WithRetry(chargePayment, middleware.RetryPolicy{
            MaxAttempts: 3,
            Backoff:     500 * time.Millisecond,
        }),
    ),
)
g.AddNode("charge", node)
```

Available middleware: `WithRecover` · `WithTimeout` · `WithRetry` · `WithCircuitBreaker` · `WithRateLimit` · `WithBulkhead` · `WithValidate` · `WithCache`

---

## More Agent Patterns

### RAG Agent

```go
ag := agent.NewRAGAgent(agent.RAGAgentConfig{
    Name:         "rag-agent",
    LLM:          adapter,
    Embedder:     embedder,
    VectorStore:  vectorStore,
    SystemPrompt: "Answer questions based on the provided documents.",
})
g, _ := ag.BuildGraph()
```

`Embedder` and `VectorStore` are any implementations of the `agent.Embedder` / `agent.VectorStore` interfaces — the framework does not bind to a specific vector database.

### Business Workflow (pure graph, no AI)

```go
g := graph.NewGraph[OrderState]("order-pipeline")
g.AddNode("validate",  validateOrder)
g.AddNode("charge",    chargePayment)
g.AddNode("fulfill",   fulfillOrder)
g.AddNode("notify",    sendNotification)
g.SetEntryPoint("validate")
g.AddEdge("validate", "charge")
g.AddEdge("charge",   "fulfill")
g.AddEdge("fulfill",  "notify")
g.Compile()

engine := graph.NewEngine(g)
result, err := engine.Run(ctx, initialState,
    graph.WithTimeout(30*time.Second),
    graph.WithCheckpoint(checkpoint.NewFileManager("/tmp/cp")),
)
if err != nil {
    // distinguish error types with graph.IsRetryableError / graph.IsCircuitOpenError
}
```

---

## Package Layout

```
graphflow/
├── graph/                  # Core graph engine (import "…/graph")
│   ├── graph.go            # Graph[S], NodeFunc, Condition, MergeFunc
│   ├── engine.go           # Engine[S].Run — Pregel superstep loop
│   ├── hooks.go            # Hook[S] interface, ComposeHooks
│   ├── errors.go           # Structured error types
│   ├── middleware/         # NodeFunc decorators
│   │   ├── retry.go
│   │   ├── circuitbreaker.go
│   │   ├── bulkhead.go
│   │   └── ...
│   ├── node/               # Built-in nodes (HTTP, Delay, Transform, Noop)
│   └── checkpoint/         # Persistence (memory · file · redis · sqlite)
│
├── agent/                  # Agent layer (import "…/agent")
│   ├── state.go            # MessageState, Message, ToolCall
│   ├── llm.go              # LLMModel, Embedder, VectorStore interfaces
│   ├── nodes.go            # LLMNode, ToolNode, VectorRetrieveNode, HumanInputNode
│   ├── tools.go            # Tool interface, ToolRegistry, CalculatorTool
│   ├── agents.go           # ReActAgent, RAGAgent, SupervisorAgent
│   ├── memory.go           # ShortTermMemory, LongTermMemory
│   └── llmgate/            # llmgate adapter (LLMModel impl)
│
├── config/                 # Configuration templates
│   └── llmgate.toml.example
│
└── examples/
    └── agent_demo/         # ReAct agent with Hook tracing (mock + real LLM)
```

---

## Roadmap

### Core (graph/)
- [x] Graph model — sequential, conditional routing
- [x] Loop / back-edge detection with iteration limits
- [x] Hook interface + ComposeHooks
- [x] Resilience middleware suite
- [x] Built-in nodes (HTTP, Delay, Transform, Noop)
- [x] Structured error types
- [x] Parallel fan-out / fan-in
- [x] Stream[T] — Send / Chan / Merge / Broadcast
- [x] Checkpoint — InMemory, File
- [ ] OTelHook
- [ ] YAML config + LoadFromFile

### Agent (agent/)
- [x] MessageState, Message, ToolCall types (A1)
- [x] LLMModel / Embedder / VectorStore interfaces (A2)
- [x] LLMNode, ToolNode — real implementation with tool calling (A3)
- [x] VectorRetrieveNode, HumanInputNode (A3)
- [x] Tool interface + ToolRegistry + CalculatorTool (A4)
- [x] ShortTermMemory, LongTermMemory (A5)
- [x] ReActAgent.BuildGraph (A6)
- [x] RAGAgent.BuildGraph (A7)
- [x] llmgate adapter — 19 providers, fallback, strategy routing
- [ ] SupervisorAgent.BuildGraph (A8)
- [ ] Stream agent + SSE (A9)
- [ ] Structured Output (A10)

---

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push and open a Pull Request

Please ensure: `go vet ./...` passes, new public APIs have doc comments, non-trivial logic has unit tests.

---

## License

[MIT](LICENSE) © 2026 Wang Zhongyou
