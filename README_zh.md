# Graphflow

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.22-blue)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/wzhongyou/graphflow)](https://goreportcard.com/report/github.com/wzhongyou/graphflow)

**Graphflow** 是一个以图执行引擎为核心的 Go 原生 Agent 开发框架。

> [English](README.md)

---

## 为什么选择 Graphflow

大多数 Agent 框架以 Python 为主，引入了沉重的运行时，且将编排逻辑与 AI 逻辑混在一起。Graphflow 的思路不同：

| | Graphflow | LangGraph | Eino |
|---|---|---|---|
| 语言 | **Go** | Python | Go |
| 核心抽象 | **图引擎** | StateGraph | Graph + Chain |
| 配置驱动 | **YAML + 代码** | 纯代码 | 纯代码 |
| 核心零外部依赖 | **是** | 否 | 否 |
| 单二进制部署 | **是** | 否 | 否 |
| 内置弹性能力 | **Retry / 熔断 / 舱壁** | 有限 | 有限 |

---

## 架构

```
┌──────────────────────────────────────────┐
│  你的应用                                 │
│  业务流程编排 · AI Agent 应用              │
└─────────────────────┬────────────────────┘
                      │
┌─────────────────────▼────────────────────┐
│  agent/   — Agent 开发层                  │
│  MessageState · LLMNode · ToolNode       │
│  ReAct · RAG · Supervisor 模式           │
│  ToolRegistry · 内存管理                  │
└─────────────────────┬────────────────────┘
                      │ 依赖
┌─────────────────────▼────────────────────┐
│  graph/   — 图执行引擎                    │
│  Graph[S] · Engine[S] · NodeFunc[S]      │
│  顺序 · 条件路由 · 并行 · 循环             │
│  Checkpoint · Hook · OTel               │
│  middleware/ · node/ · checkpoint/       │
└──────────────────────────────────────────┘
```

`graph/` 包**与 AI 完全无关**——同样适用于业务流程编排、ETL 管道、事件驱动架构。`agent/` 包在其上提供 AI 专属的抽象层。

---

## 快速开始

### ReAct Agent

```go
import (
    "github.com/wzhongyou/graphflow/agent"
    "github.com/wzhongyou/graphflow/graph"
)

// 1. 构建节点
llm      := agent.NewLLMNode(agent.LLMNodeConfig{Model: myLLM})
toolNode := agent.NewToolNode(calculatorTool, searchTool)

// 2. 连接 ReAct 图
g := graph.NewGraph[*agent.MessageState]("react-agent")
g.AddNode("llm", llm.Run)
g.AddNode("tool", toolNode.Run)
g.SetEntryPoint("llm")
g.AddCondition("llm", graph.Condition[*agent.MessageState]{
    If:     hasPendingToolCalls,
    Target: "tool",
})
g.AddEdge("tool", "llm")   // 回边，形成循环
g.SetMaxIterations("llm", 20)
g.Compile()

// 3. 用 Hook 追踪 Thought / Action / Observation
engine := graph.NewEngine(g)
result, _ := engine.Run(ctx, &agent.MessageState{
    Messages: []agent.Message{{Role: agent.RoleUser, Content: "123 * 456 是多少？"}},
}, graph.WithHook(myReactHook))
```

### 用 Hook 观测 ReAct 循环

```go
type reactHook struct{}

func (h *reactHook) OnNodeEnd(_ context.Context, node string, s *agent.MessageState, _ error, dur time.Duration) {
    last := s.Messages[len(s.Messages)-1]
    switch node {
    case "llm":
        if len(last.ToolCalls) > 0 {
            fmt.Printf("[思考] → 调用工具: %s  (%s)\n", last.ToolCalls[0].Name, dur)
        } else {
            fmt.Printf("[思考] → 最终回答: %s\n", last.Content)
        }
    case "tool":
        for _, m := range s.Messages {
            if m.Role == agent.RoleTool {
                fmt.Printf("[观察] %s: %s\n", m.ToolName, m.Content)
            }
        }
    }
}
```

输出示例：
```
[react-agent] 问题: 123 * 456 是多少？
  [思考] → 调用工具: calculator  (12ms)
  [观察] calculator: 56088
  [思考] → 最终回答: 答案是 56088。

[完成] steps=3  duration=21ms
```

### 业务工作流（纯图，无 AI）

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

## 弹性中间件

节点函数是普通的 `func(ctx, state) (state, error)`——直接用中间件包装：

```go
// 推荐组合顺序（由外到内）
node := middleware.WithRecover("payment",          // panic → error
    middleware.WithTimeout(chargePayment, 5*time.Second,
        middleware.WithRetry(chargePayment, middleware.RetryPolicy{
            MaxAttempts: 3,
            Backoff:     500 * time.Millisecond,
        }),
    ),
)
g.AddNode("charge", node)
```

可用中间件：`WithRecover` · `WithTimeout` · `WithRetry` · `WithCircuitBreaker` · `WithRateLimit` · `WithBulkhead` · `WithValidate` · `WithCache`

---

## 包结构

```
graphflow/
├── graph/                  # 核心图引擎（import "…/graph"）
│   ├── graph.go            # Graph[S]、NodeFunc、Condition、MergeFunc
│   ├── engine.go           # Engine[S].Run — Pregel 超级步执行循环
│   ├── hooks.go            # Hook[S] 接口、ComposeHooks
│   ├── errors.go           # 结构化错误类型
│   ├── middleware/         # NodeFunc 装饰器
│   │   ├── retry.go
│   │   ├── circuitbreaker.go
│   │   ├── bulkhead.go
│   │   └── ...
│   ├── node/               # 内置节点（HTTP、Delay、Transform、Noop）
│   └── checkpoint/         # 持久化（内存 · 文件 · Redis · SQLite）
│
├── agent/                  # Agent 层（import "…/agent"）
│   ├── state.go            # MessageState、Message、ToolCall
│   ├── llm.go              # LLMModel、Embedder、VectorStore 接口
│   ├── nodes.go            # LLMNode、ToolNode、VectorRetrieveNode、HumanInputNode
│   ├── tools.go            # Tool 接口、ToolRegistry
│   ├── agents.go           # ReActAgent、RAGAgent、SupervisorAgent
│   └── memory.go           # ShortTermMemory、LongTermMemory
│
└── examples/
    └── agent_demo/         # 带 Hook 追踪的 ReAct Agent 示例
```

---

## 路线图

### 核心层（graph/）
- [x] 图模型——顺序执行、条件路由
- [x] 循环 / 回边检测与迭代限制
- [x] Hook 接口 + ComposeHooks
- [x] 弹性中间件套件
- [x] 内置节点（HTTP、Delay、Transform、Noop）
- [x] 结构化错误类型
- [x] 并行扇出 / 扇入
- [x] Stream[T] — Send / Chan / Merge / Broadcast
- [x] Checkpoint——内存、文件
- [ ] OTelHook
- [ ] YAML 配置 + LoadFromFile

### Agent 层（agent/）
- [x] MessageState、Message、ToolCall 类型
- [x] LLMModel / Embedder / VectorStore 接口
- [x] LLMNode、ToolNode 等节点骨架
- [x] ToolRegistry
- [ ] LLMNode 真正实现（A3）
- [ ] ToolNode 真正实现（A3）
- [ ] ReActAgent.BuildGraph（A6）
- [ ] RAGAgent.BuildGraph（A7）
- [ ] SupervisorAgent.BuildGraph（A8）
- [ ] 内存管理（A5）

---

## 贡献指南

1. Fork 本仓库
2. 创建特性分支（`git checkout -b feature/amazing-feature`）
3. 提交更改（`git commit -m 'Add amazing feature'`）
4. 推送并发起 Pull Request

请确保：`go vet ./...` 通过，新公开 API 有文档注释，非简单逻辑有单元测试。

---

## 许可证

[MIT](LICENSE) © 2026 Wang Zhongyou
