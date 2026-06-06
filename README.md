# Graphflow

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/wzhongyou/graphflow.svg)](https://pkg.go.dev/github.com/wzhongyou/graphflow)

**Graphflow** 是一个 Go 原生的通用图执行引擎，面向后端服务编排、工作流引擎、ETL 管道等场景。

> 心智模型：**图 = 节点 + 边 + 状态机**。节点处理状态，边控制流转，引擎按 Pregel 超级步循环执行。跟写普通 Go 函数一样简单。

---

## 安装

```bash
go get github.com/wzhongyou/graphflow
```

需要 Go 1.21+。核心引擎 `graph/` 零外部依赖。

---

## 5 秒跑起来

```bash
go run ./examples/workflow
```

输出：

```
═══ 场景 1: 正常订单 ═══
[validate] 校验订单 ORD-001 (金额: 299.00)...
  ✓ 校验通过
[payment] 处理支付 299.00...
  ✓ 支付成功
[fulfill] 发货中...
  ✓ 已发货
[notify] 发送通知...
  ✓ 通知已发送
[正常订单] 完成
  执行步骤: [validated paid fulfilled notified]
  耗时: 434ms
```

一个订单处理管线：校验 → 支付 → 条件路由（成功/失败）→ 发货 → 通知。

---

## 什么时候用 Graphflow

| 场景 | 适合吗 | 说明 |
|------|--------|------|
| 微服务编排（订单处理、审批流） | **最适合** | 图结构天然匹配业务流程 |
| ETL / 数据管道 | **最适合** | 并行扇出 + 条件路由 |
| 事件驱动架构 | 适合 | Hook 机制可观测每步执行 |
| AI Agent 编排 | 适合 | 配合 [Cangjie](https://github.com/wzhongyou/cangjie) 使用 |
| 需要弹性保障的生产环境 | **最适合** | 内置熔断、重试、超时、舱壁 |
| Python 生态重度依赖 | 不适合 | 考虑 Temporal / Airflow |

---

## 核心概念

### 节点函数

```go
// NodeFunc 接收状态，返回更新后的状态
type NodeFunc[S any] func(ctx context.Context, state S) (S, error)
```

所有节点共享同一个状态类型 `S`，编译时保证类型安全。

### 边

| 类型 | 语义 | 用法 |
|------|------|------|
| **普通边** | A 完成后无条件转移到 B | `g.AddEdge("A", "B")` |
| **条件边** | A 完成后根据状态选择目标 | `g.AddCondition("A", ...)` |
| **回边** | 回指上游，形成循环 | `g.AddEdge("B", "A")` |
| **并行边** | A 有多个出边，自动扇出 | `g.AddEdge("A", "B")` + `g.AddEdge("A", "C")` |

---

## 快速开始

### 1. 定义状态和节点

```go
type OrderState struct {
    OrderID string
    Amount  float64
    Paid    bool
    Steps   []string
}

func processPayment(ctx context.Context, s OrderState) (OrderState, error) {
    if s.Amount > 10000 {
        return s, fmt.Errorf("余额不足")
    }
    s.Paid = true
    return s, nil
}
```

### 2. 构建图

```go
g := graph.NewGraph[OrderState]("order-pipeline")

g.AddNode("validate", validateOrder)
g.AddNode("payment",  processPayment)
g.AddNode("fulfill",  fulfillOrder)
g.AddNode("cancel",   cancelOrder)
g.AddNode("notify",   sendNotification)

g.SetEntryPoint("validate")
g.AddEdge("validate", "payment")

// 条件路由：失败 → cancel，成功 → fulfill
g.AddCondition("payment", graph.Condition[OrderState]{
    If:     func(ctx context.Context, s OrderState) bool { return s.Error != "" },
    Target: "cancel",
})
g.AddEdge("payment", "fulfill") // 无条件走 fulfill（"else" 分支）

g.AddEdge("fulfill", "notify")
g.Compile()
```

### 3. 执行

```go
engine := graph.NewEngine(g)
result, err := engine.Run(ctx, OrderState{
    OrderID: "ORD-001",
    Amount:  299.00,
})
fmt.Println(result.FinalState.Steps) // [validated paid fulfilled notified]
```

---

## 弹性中间件

节点函数是普通的 `func(ctx, state) (state, error)`——直接用中间件包装：

```go
node := middleware.WithRecover("payment",
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

## Hook 可观测

```go
type myHook struct{}

func (h *myHook) OnNodeStart(ctx context.Context, node string, s MyState) {
    log.Printf("[%s] 开始执行", node)
}
func (h *myHook) OnNodeEnd(ctx context.Context, node string, s MyState, err error, d time.Duration) {
    log.Printf("[%s] 完成 (%v)", node, d)
}

engine.Run(ctx, state, graph.WithHook(&myHook{}))
```

Hook 接口：`OnGraphStart` · `OnGraphEnd` · `OnNodeStart` · `OnNodeEnd` · `OnError`。多个 Hook 可用 `graph.ComposeHooks` 组合。

内置 Hook：`graph/hooks/otel.go` — OpenTelemetry 追踪，开箱即用。

---

## Checkpoint 持久化

```go
// 内存（开发/测试）
cp := checkpoint.NewMemoryManager()

// 文件（单机生产）
cp := checkpoint.NewFileManager("/var/graphflow/checkpoints")

// Redis（分布式）
cp := checkpoint.NewRedisManager("redis://localhost:6379")

// SQLite（嵌入式）
cp := checkpoint.NewSQLiteManager("/var/graphflow/state.db")

engine.Run(ctx, state, graph.WithCheckpoint(cp))
```

节点失败后可从上次 checkpoint 恢复，无需从头重跑。

---

## 流式执行

```go
stream, _ := engine.RunStream(ctx, initialState)
for event := range stream.Chan() {
    switch event.Type {
    case graph.StreamNodeStart:
        fmt.Printf("▶ %s 开始\n", event.NodeName)
    case graph.StreamNodeEnd:
        fmt.Printf("■ %s 完成 (%v)\n", event.NodeName, event.Duration)
    case graph.StreamGraphEnd:
        fmt.Println("◆ 图执行完成")
    }
}
```

---

## 常用执行选项

```go
engine.Run(ctx, state,
    graph.WithTimeout(30*time.Second),
    graph.WithCheckpoint(cp),
    graph.WithAutoCheckpoint(1000), // 每 1000 步自动保存
    graph.WithMaxIterations(500),
    graph.WithHook(myHook),
)
```

---

## 包结构

```
graphflow/
├── graph/                  # 核心图引擎（import "…/graph"）
│   ├── graph.go            # Graph[S]、NodeFunc、Condition、MergeFunc
│   ├── engine.go           # Engine[S].Run — Pregel 超级步执行循环
│   ├── engine_parallel.go  # 并行扇出 / 扇入
│   ├── engine_loop.go      # 循环 / 回边执行
│   ├── hooks.go            # Hook[S] 接口、ComposeHooks
│   ├── errors.go           # 结构化错误类型
│   ├── stream.go           # Stream[T] — Send / Chan / Merge / Broadcast
│   ├── config.go           # YAML 配置加载
│   ├── registry.go         # 节点注册表
│   ├── result.go           # ExecutionResult[S]
│   ├── middleware/         # NodeFunc 装饰器
│   │   ├── retry.go
│   │   ├── circuitbreaker.go
│   │   ├── bulkhead.go
│   │   ├── timeout.go
│   │   ├── ratelimit.go
│   │   ├── recover.go
│   │   ├── validate.go
│   │   └── cache.go
│   ├── node/               # 内置节点
│   │   ├── http.go          # HTTP 请求节点
│   │   ├── delay.go         # 延迟节点
│   │   ├── transform.go     # 状态转换节点
│   │   └── noop.go          # 空操作节点
│   ├── checkpoint/         # 持久化后端
│   │   ├── memory.go        # 内存
│   │   ├── file.go          # 文件
│   │   ├── redis/           # Redis
│   │   └── sqlite/          # SQLite
│   └── hooks/
│       └── otel.go          # OpenTelemetry Hook
```

---

## 与其他方案对比

| | Graphflow | Temporal | Conductor | Uber Cadence |
|---|---|---|---|---|
| 语言 | **Go** | 多语言 SDK | Java | Go / Java |
| 部署模型 | **库嵌入** | Server + Worker | Server | Server |
| 核心零外部依赖 | **是** | 否 | 否 | 否 |
| 单二进制 | **是** | 否 | 否 | 否 |
| 弹性中间件 | **8 种** | 内置 | 有限 | 有限 |
| 复杂度 | 低 | 高 | 中 | 高 |

Graphflow 不是 Temporal 的替代品——Temporal 有分布式调度、多语言 SDK、可视化管理台。Graphflow 的定位是**库级图引擎**：不需要独立服务，`go get` 即用，适合嵌入已有 Go 服务做内部编排。

---

## 与 Cangjie 的关系

[**Cangjie（仓颉）**](https://github.com/wzhongyou/cangjie) 是基于 Graphflow 构建的终端 AI 编程助手。Graphflow 提供图执行引擎，Cangjie 在其上构建 Agent 循环、工具系统、TUI 界面。

```
Graphflow  → 图执行引擎（这个仓库）
Cangjie    → AI 编程助手（import graphflow）
```

---

## 路线图

- [x] 图模型——顺序执行、条件路由、循环
- [x] 并行扇出 / 扇入
- [x] Hook 接口 + OpenTelemetry 追踪
- [x] 弹性中间件套件（8 种）
- [x] 内置节点（HTTP、Delay、Transform、Noop）
- [x] Checkpoint 持久化（4 种后端）
- [x] 流式执行事件
- [x] YAML 配置加载 + 节点注册表
- [ ] 分布式执行
- [ ] 可视化编排调试

---

## 贡献指南

1. Fork 本仓库
2. 创建特性分支
3. 提交更改
4. 推送并发起 Pull Request

请确保：`go vet ./...` 通过，新公开 API 有文档注释，非简单逻辑有单元测试。

---

## 许可证

[MIT](LICENSE) © 2026 Wang Zhongyou
