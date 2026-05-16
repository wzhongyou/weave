# Graphflow

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/wzhongyou/graphflow)](https://goreportcard.com/report/github.com/wzhongyou/graphflow)
[![Go Reference](https://pkg.go.dev/badge/github.com/wzhongyou/graphflow.svg)](https://pkg.go.dev/github.com/wzhongyou/graphflow)

**Graphflow** 是一个以图执行引擎为核心的 Go 原生 Agent 开发框架。

> 心智模型：**图 = 节点 + 边 + 状态机**。节点处理状态，边控制流转，引擎按 Pregel 超级步循环执行。跟写普通 Go 函数一样简单。

> [English](README.md)

---

## 安装

```bash
go get github.com/wzhongyou/graphflow
```

需要 Go 1.21+。核心引擎 `graph/` 零外部依赖。

---

## 5 秒跑起来

```bash
go run ./examples/agent_demo
```

无需任何配置，Mock 模式直接运行：

```
[react-agent] 问题: What is 123 * 456?
  [思考] → 调用工具: calculator  (12ms)
  [观察] calculator: 56088
  [思考] → 最终回答: 答案是 56088。

[done] steps=3  duration=21ms
```

这就是一个 ReAct Agent：LLM 思考 → 调用计算器 → 返回结果。

---

## 什么时候用 Graphflow

| 场景 | 适合吗 | 说明 |
|------|--------|------|
| 构建 AI Agent（工具调用、RAG、多步推理） | **最适合** | ReAct / RAG 开箱即用 |
| 业务工作流编排（订单处理、ETL） | 适合 | 纯图引擎，跟 AI 无关 |
| 需要非 Python 部署环境 | **最适合** | 单 Go 二进制，无 Python 运行时 |
| 需要弹性保障的生产环境 | **最适合** | 内置熔断、重试、超时、舱壁 |
| Python 生态重度依赖（LangChain 等） | 不适合 | 考虑 LangGraph |

---

## 为什么选择 Graphflow

大多数 Agent 框架以 Python 为主，引入了沉重的运行时，且将编排逻辑与 AI 逻辑混在一起。Graphflow 的思路不同：

| | Graphflow | LangGraph | Eino |
|---|---|---|---|
| 语言 | **Go** | Python | Go |
| 核心抽象 | **图引擎** | StateGraph | Graph + Chain |
| 无 Python 运行时 | **是** | 否 | **是** |
| 核心零外部依赖 | **是** | 否 | 否 |
| 内置弹性能力 | **熔断 / 重试 / 舱壁 / 限流** | 有限 | 有限 |

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

### 1. Mock 模式（零配置，先跑起来）

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
        SystemPrompt: "你是一个有用的助手。",
        Tools:        []agent.Tool{&agent.CalculatorTool{}},
    })
    g, err := ag.BuildGraph()
    if err != nil {
        panic(err)
    }

    engine := graph.NewEngine(g)
    result, err := engine.Run(context.Background(), &agent.MessageState{
        Messages: []agent.Message{{Role: agent.RoleUser, Content: "100 + 200 是多少？"}},
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(result.FinalState.Messages[len(result.FinalState.Messages)-1].Content)
}
```

不传 `LLM` 字段，自动使用 Mock 模型（内置计算器），无需 API key。

### 2. 接入真实 LLM

Graphflow 通过 [llmgate](https://github.com/wzhongyou/llmgate) 接入 LLM（支持 19 个供应商）。

```bash
# 方式一：配置文件（推荐）
cp config/llmgate.toml.example config/llmgate.toml   # 填入 API key
go run ./examples/agent_demo -q "100 + 200 是多少？"

# 方式二：环境变量
export DEEPSEEK_KEY="sk-xxx"
go run ./examples/agent_demo -env -provider deepseek -q "100 + 200 是多少？"
```

代码只需加 3 行：

```go
import llmgate_adapter "github.com/wzhongyou/graphflow/agent/llmgate"
import "github.com/wzhongyou/llmgate/sdk"

gw, _ := sdk.NewFromFile("config/llmgate.toml")
adapter := llmgate_adapter.New(gw, llmgate_adapter.Config{Provider: "deepseek"})

ag := agent.NewReActAgent(agent.ReActAgentConfig{
    LLM: adapter,  // 传入 LLM 即可
    // ...
})
```

### 3. 手动搭图（更多控制）

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
g.AddEdge("tool", "llm")   // 回边，形成循环
g.SetMaxIterations("llm", 20)
g.Compile()
```

---

## 用 Hook 观测执行过程

实现 `graph.Hook` 接口即可观测每一步的执行：

```go
type reactHook struct{}

func (h *reactHook) OnNodeStart(_ context.Context, node string, s *agent.MessageState) {
    if node == "llm" {
        fmt.Printf("[开始] LLM 思考中...\n")
    }
}

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

// 使用
engine.Run(ctx, state, graph.WithHook(&reactHook{}))
```

Hook 接口完整方法：`OnGraphStart` · `OnGraphEnd` · `OnNodeStart` · `OnNodeEnd` · `OnError`。多个 Hook 可用 `graph.ComposeHooks` 组合。

---

## 弹性中间件

节点函数是普通的 `func(ctx, state) (state, error)`——直接用中间件包装：

```go
// 推荐组合顺序（由外到内）
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

可用中间件：`WithRecover` · `WithTimeout` · `WithRetry` · `WithCircuitBreaker` · `WithRateLimit` · `WithBulkhead` · `WithValidate` · `WithCache`

---

## 更多 Agent 模式

### RAG Agent

```go
ag := agent.NewRAGAgent(agent.RAGAgentConfig{
    Name:         "rag-agent",
    LLM:          adapter,
    Embedder:     embedder,
    VectorStore:  vectorStore,
    SystemPrompt: "基于提供的文档回答问题。",
})
g, _ := ag.BuildGraph()
```

`Embedder` 和 `VectorStore` 是实现 `agent.Embedder` / `agent.VectorStore` 接口的任意实现，框架不绑定具体向量数据库。

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
if err != nil {
    // 可通过 graph.IsRetryableError / graph.IsCircuitOpenError 区分错误类型
}
```

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
│   ├── tools.go            # Tool 接口、ToolRegistry、CalculatorTool
│   ├── agents.go           # ReActAgent、RAGAgent、SupervisorAgent
│   ├── memory.go           # ShortTermMemory、LongTermMemory
│   └── llmgate/            # llmgate 适配器（实现 LLMModel 接口）
│
├── config/                 # 配置模板
│   └── llmgate.toml.example
│
└── examples/
    └── agent_demo/         # 带 Hook 追踪的 ReAct Agent 示例（mock + 真实 LLM）
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
- [x] MessageState、Message、ToolCall 类型（A1）
- [x] LLMModel / Embedder / VectorStore 接口（A2）
- [x] LLMNode、ToolNode 真实实现，支持 tool calling（A3）
- [x] VectorRetrieveNode、HumanInputNode（A3）
- [x] Tool 接口 + ToolRegistry + CalculatorTool（A4）
- [x] ShortTermMemory、LongTermMemory（A5）
- [x] ReActAgent.BuildGraph（A6）
- [x] RAGAgent.BuildGraph（A7）
- [x] llmgate 适配器 — 19 个供应商、降级、策略路由
- [ ] SupervisorAgent.BuildGraph（A8）
- [ ] 流式 Agent + SSE（A9）
- [ ] Structured Output（A10）

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
