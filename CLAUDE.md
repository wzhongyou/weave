# CLAUDE.md

## 项目概述

Weave 是一个 Go 原生的通用图执行引擎，面向后端服务编排、工作流引擎、ETL 管道等场景。`graph/` 是唯一核心包，**与 AI 完全无关**。

> AI Agent 开发请使用 [Cangjie（仓颉）](https://github.com/wzhongyou/cangjie)，它基于 Weave 图引擎构建。

## 常用命令

```bash
go build ./...          # 构建所有包
go vet ./...            # 静态分析（提交前必须通过）
go test ./...           # 运行所有测试
go test ./graph/...     # 只运行图引擎测试

# 运行工作流示例
go run ./examples/workflow
```

## 包结构

```
graph/          # 核心引擎 — Graph[S], Engine[S], NodeFunc[S]
  middleware/   # NodeFunc 装饰器（重试、超时、熔断器……）
  node/         # 内置节点（HTTP、Delay、Transform、Noop）
  checkpoint/   # 持久化后端（内存、文件、Redis、SQLite）
  hooks/        # OpenTelemetry Hook
examples/
  workflow/     # 业务工作流示例（订单处理管线）
docs/           # 设计文档和指南
```

## 关键约定

- `graph/` **零外部依赖**——不要添加任何 stdlib 之外的 import。
- 外部集成（Redis、SQLite、OTel）放在子包中，保持核心层无依赖。
- 所有公开类型都使用 Go 泛型（`Graph[S]`、`Engine[S]`、`Hook[S]`）——每个图保持状态类型 `S` 作为唯一泛型参数。
- 节点函数签名严格匹配：`func(ctx context.Context, state S) (S, error)`。
- 中间件包装 `NodeFunc[S]` 并返回 `NodeFunc[S]`——可组合的设计。

## 实现状态

使用 `TODO(Px)` 标记，对应设计文档中的路线图阶段：

| 标记 | 含义 | 状态 |
|--------|---------|--------|
| `TODO(P1)` | Core P1 — 图模型、顺序引擎、YAML 配置 | 核心已完成 |
| `TODO(P7)` | OTel Hook | 已完成 |

Agent 相关代码（`TODO(Ax)` 系列）已迁出至 [Cangjie](https://github.com/wzhongyou/cangjie)。

## 设计决策

完整说明见 `docs/weave-design.md`。关键几点：

- **Pregel 风格执行**：超级步循环，非递归调用。
- **回边 = 循环**：`Compile()` 时通过 DFS 检测；`SetMaxIterations` 防止死循环（默认 1000）。
- **条件边优先**于无条件边；第一个匹配的条件胜出。
- **多条无条件边 = 扇出**（并行，在 `engine_parallel.go` 中实现）。
- **Hook 存储为 `any`** 在 `runConfig` 中，通过 `hookOf[S]` 类型断言——避免让 `Option` 变成泛型，代价是类型不匹配时静默无操作。
