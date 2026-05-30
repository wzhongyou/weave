# CLAUDE.md

## 项目概述

Graphflow 是一个 Go 原生的 Agent 开发框架。图执行引擎（`graph/`）是 AI 无关的核心；Agent 层（`agent/`）构建在其之上。LLM 访问通过 [llmgate](https://github.com/wzhongyou/llmgate) 网关（`agent/llmgate/` 适配器）。

## 常用命令

```bash
go build ./...          # 构建所有包
go vet ./...            # 静态分析（提交前必须通过）
go test ./...           # 运行所有测试
go test ./agent/...     # 只运行 Agent 测试
go test ./graph/...     # 只运行图引擎测试

# 运行 Agent 示例（Mock 模式，无需 API key）
go run ./examples/agent_demo

# 用真实 LLM 运行 Agent 示例（需要 config/llmgate.toml）
go run ./examples/agent_demo -q "计算 100 + 200"
```

## 包结构

```
graph/          # 核心引擎 — Graph[S], Engine[S], NodeFunc[S]
  middleware/   # NodeFunc 装饰器（重试、超时、熔断器……）
  node/         # 内置节点（HTTP、Delay、Transform、Noop）
  checkpoint/   # 持久化后端（内存、文件、Redis、SQLite）
agent/          # Agent 抽象层 — MessageState, LLMNode, ToolNode……
  llmgate/      # llmgate SDK 适配器（实现 agent.LLMModel）
config/         # 配置模板（llmgate.toml.example）
examples/
  agent_demo/   # 带 Hook 追踪的 ReAct Agent（mock + 真实 LLM）
docs/           # 设计文档和指南
```

## 关键约定

- `graph/` **零外部依赖**——不要添加任何 stdlib 之外的 import。
- `agent/` 可以 import `graph/`，反之不行。
- `agent/llmgate/` import 了 `github.com/wzhongyou/llmgate`（Agent 层唯一的外部依赖）。
- 外部集成（Redis、SQLite、OTel）放在子包中，保持核心层无依赖。
- 所有公开类型都使用 Go 泛型（`Graph[S]`、`Engine[S]`、`Hook[S]`）——每个图保持状态类型 `S` 作为唯一泛型参数。
- 节点函数签名严格匹配：`func(ctx context.Context, state S) (S, error)`。
- 中间件包装 `NodeFunc[S]` 并返回 `NodeFunc[S]`——可组合的设计。

## 实现状态

使用 `TODO(Px)` / `TODO(Ax)` 标记，对应设计文档中的路线图阶段：

| 标记 | 含义 | 状态 |
|--------|---------|--------|
| `TODO(P1)` | Core P1 — 图模型、顺序引擎、YAML 配置 | 核心已完成；YAML 待做 |
| `TODO(P7)` | OTel Hook | 未开始 |
| `TODO(A8)` | SupervisorAgent.BuildGraph | 仅存桩 |
| `TODO(A9)+` | 流式、结构化输出等 | 未开始 |

阶段 **A1–A7 已完成**（MessageState、LLMModel、LLMNode、ToolNode、ToolRegistry、CalculatorTool、ShortTermMemory、LongTermMemory、ReActAgent、RAGAgent、llmgate 适配器）。

## LLM 配置

llmgate 配置放在 `config/llmgate.toml`（已 .gitignore）。模板在 `config/llmgate.toml.example`。三种设置模式：

1. 配置文件：自动检测 `config/llmgate.toml` 或 `llmgate.toml`
2. 环境变量：`DEEPSEEK_KEY=sk-xxx`（自动检测）
3. Mock 模式：找不到配置文件或 key 时自动降级

## 设计决策

完整说明见 `docs/graphflow-design.md`。关键几点：

- **Pregel 风格执行**：超级步循环，非递归调用。
- **回边 = 循环**：`Compile()` 时通过 DFS 检测；`SetMaxIterations` 防止死循环（默认 1000）。
- **条件边优先**于无条件边；第一个匹配的条件胜出。
- **多条无条件边 = 扇出**（并行，在 `engine_parallel.go` 中实现）。
- **Hook 存储为 `any`** 在 `runConfig` 中，通过 `hookOf[S]` 类型断言——避免让 `Option` 变成泛型，代价是类型不匹配时静默无操作。
- **LLMModel 接口与供应商无关**；llmgate 适配器处理供应商差异（OpenAI/Anthropic/Gemini 协议映射、降级、策略路由）。
