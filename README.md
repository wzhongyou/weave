# Graphflow

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.22-blue)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/wzhongyou/graphflow)](https://goreportcard.com/report/github.com/wzhongyou/graphflow)

**Graphflow** is a Go-native Agent development framework built on a type-safe graph execution engine.

> [中文文档](README_zh.md)

---

## Why Graphflow

Most Agent frameworks are Python-first, bring heavy runtimes, and blur the line between orchestration and AI logic. Graphflow is different:

| | Graphflow | LangGraph | Eino |
|---|---|---|---|
| Language | **Go** | Python | Go |
| Core abstraction | **Graph engine** | StateGraph | Graph + Chain |
| Config-driven | **YAML + code** | Code only | Code only |
| Zero-dependency core | **Yes** | No | No |
| Single binary deploy | **Yes** | No | No |
| Built-in resilience | **Retry / CB / Bulkhead** | Limited | Limited |

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

### ReAct Agent

```go
import (
    "github.com/wzhongyou/graphflow/agent"
    "github.com/wzhongyou/graphflow/graph"
)

// 1. Build nodes
llm      := agent.NewLLMNode(agent.LLMNodeConfig{Model: myLLM})
toolNode := agent.NewToolNode(calculatorTool, searchTool)

// 2. Wire the ReAct graph
g := graph.NewGraph[*agent.MessageState]("react-agent")
g.AddNode("llm", llm.Run)
g.AddNode("tool", toolNode.Run)
g.SetEntryPoint("llm")
g.AddCondition("llm", graph.Condition[*agent.MessageState]{
    If:     hasPendingToolCalls,
    Target: "tool",
})
g.AddEdge("tool", "llm")   // loop back
g.SetMaxIterations("llm", 20)
g.Compile()

// 3. Run with a hook that traces Thought / Action / Observation
engine := graph.NewEngine(g)
result, _ := engine.Run(ctx, &agent.MessageState{
    Messages: []agent.Message{{Role: agent.RoleUser, Content: "What is 123 * 456?"}},
}, graph.WithHook(myReactHook))
```

### Observing the ReAct loop with Hook

```go
type reactHook struct{}

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
// ... other Hook methods
```

Output:
```
[react-agent] question: What is 123 * 456?
  [Thought] → Action: calculator  (12ms)
  [Observation] calculator: 56088
  [Thought] → Answer: The answer is 56088.

[done] steps=3  duration=21ms
```

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
```

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
│   ├── tools.go            # Tool interface, ToolRegistry
│   ├── agents.go           # ReActAgent, RAGAgent, SupervisorAgent
│   └── memory.go           # ShortTermMemory, LongTermMemory
│
└── examples/
    └── agent_demo/         # ReAct agent with Hook tracing
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
- [x] MessageState, Message, ToolCall types
- [x] LLMModel / Embedder / VectorStore interfaces
- [x] LLMNode, ToolNode, VectorRetrieveNode, HumanInputNode stubs
- [x] ToolRegistry
- [ ] LLMNode — real implementation (A3)
- [ ] ToolNode — real implementation (A3)
- [ ] ReActAgent.BuildGraph (A6)
- [ ] RAGAgent.BuildGraph (A7)
- [ ] SupervisorAgent.BuildGraph (A8)
- [ ] Memory management (A5)

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
