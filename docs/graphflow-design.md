# Graphflow 架构设计文档

> 状态：草案，讨论中（v0.2 - 增加Agent层设计）

## 1. 定位与目标

**Graphflow** 是一个 Go 语言实现的通用工作流编排引擎，采用分层架构设计：

```
┌─────────────────────────────────────────────┐
│     应用层（用户代码）                         │
│  - 业务流程编排                               │
│  - AI Agent 应用                             │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│  Agent Layer (graphflow/agent 包)            │
│  - 预置 Agent 节点                            │
│  - Agent 模式封装                             │
│  - 工具系统                                   │
│  - 内存管理                                   │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│  Core Layer (graphflow 包)                   │
│  - 通用图执行引擎                              │
│  - 配置驱动 + 编程式 API                       │
│  - 并行/循环/条件路由                          │
│  - Checkpoint + Hook + OTel                   │
└─────────────────────────────────────────────┘
```

**核心定位**：
- **Core 层**：完全 AI-无关的通用图执行引擎，适用于业务编排、微服务编排、ETL、事件驱动架构等场景
- **Agent 层**：基于 Core 层的 AI Agent 专用抽象层，提供 LLM 节点、工具系统、Agent 模式等

**设计原则**：

- **分层解耦**：Core 层与 Agent 层低耦合，Agent 层依赖 Core 层，但 Core 层不依赖 Agent 层
- **配置驱动**：图结构通过 YAML 配置文件定义，与代码解耦；节点实现通过注册表注入
- **类型安全**：利用 Go 泛型，编译时保证图中所有节点操作同一状态类型
- **最小依赖**：Core 层零外部依赖，可观测性和持久化以接口方式可插拔
- **渐进复杂度**：简单场景（线性链）写法简单，复杂场景（并行/循环/条件）可组合实现
- **单二进制部署**：不引入 Python 运行时、不依赖外部服务即可运行
- **部署灵活**：作为库嵌入用户服务，不提供独立 Server（可通过示例代码展示如何作为 Server 使用）

**目标用户**：

1. **业务开发团队**：使用 Core 层编排微服务、ETL、事件驱动流程
2. **AI 开发团队**：使用 Agent 层构建 RAG、多 Agent 协作、HITL 应用
3. **混合场景**：同一系统既有业务编排又有 Agent 能力

---

## 2. 核心抽象

### 2.1 节点（Node）

节点是图的最小执行单元，签名极简：

```go
// NodeFunc 接收状态，返回更新后的状态，以及可能的错误
type NodeFunc[S any] func(ctx context.Context, state S) (S, error)
```

所有节点共享同一个状态类型 `S`，由图的泛型参数统一约束。每个节点看到的是完整状态，自由选择要读写的字段。

### 2.2 边（Edge）

边定义了节点间的转移关系，分为三种：

| 类型 | 语义 | 示例 |
|------|------|------|
| **普通边** | A 完成后无条件转移到 B | `A -> B` |
| **条件边** | A 完成后根据状态值选择目标 | `A -> B(if x>0), C(else)` |
| **回边（Back Edge）** | 回指到上游节点，形成循环 | `B -> A`（A 是循环入口） |

### 2.3 状态（State）

完全由用户定义，引擎不感知状态内部结构。引擎需要的是两个可选能力：

```go
// 序列化：用于 checkpoint，默认走 gob
type StateSerializer interface {
    MarshalState() ([]byte, error)
    UnmarshalState([]byte) error
}

// 深拷贝：用于并行分支隔离，默认走 gob 往返
type StateDeepCopier[S any] interface {
    DeepCopy() S
}
```

### 2.4 合并函数（MergeFunc）

多个并行分支汇聚到同一节点时，需要一个函数将各分支的状态合并为一个：

```go
// parent: 分叉点时的状态快照
// branches: 各分支执行后的状态，顺序与边定义顺序一致
type MergeFunc[S any] func(ctx context.Context, parent S, branches []S) (S, error)
```

如果用户未提供 MergeFunc，引擎使用默认策略：**按分支顺序覆盖，最后一个分支的状态为主状态**。

---

## 3. 配置驱动：图定义方式

Graphflow 提供两种定义图的方式，它们可以混合使用：

| 方式 | 适用场景 | 优势 |
|------|---------|------|
| **YAML 配置文件** | 静态图、生产部署、团队协作 | 可读性强、图结构一目了然、方便版本管理 |
| **编程式 API** | 动态图、测试、复杂条件逻辑 | 灵活、可程序化生成图结构 |

两者的关系：**配置文件定义结构，代码注册实现**。这与 CI/CD 流水线（GitHub Actions YAML）、Google ADK Go 的声明式配置是同一思路。

### 3.1 配置文件格式（YAML）

```yaml
# workflow.yaml
name: text-analysis-pipeline
version: "1"
entry: tokenize

nodes:
  - name: tokenize
    type: tokenizer           # 对应 Registry 中注册的节点类型
    timeout: 10s
    retry:
      max_attempts: 3
      backoff: 1s
      max_backoff: 30s

  - name: analyze_positive
    type: sentiment_analyzer

  - name: analyze_negative
    type: sentiment_analyzer

  - name: summarize
    type: summarizer

edges:
  # 普通边：无条件转移
  - from: tokenize
    to: classify

  # 条件边：根据条件函数路由
  - from: classify
    condition: score_check
    branches:
      - when: positive
        to: analyze_positive
      - when: negative
        to: analyze_negative

  # 并行扇出（from 有多个 target，自动并行）
  - from: tokenize
    to:
      - analyze_positive
      - analyze_negative

  # 扇入汇聚（多个 from 指向同一个 to）
  - from:
      - analyze_positive
      - analyze_negative
    to: summarize
    merge: result_merger       # 对应 Registry 中注册的合并函数

  # 回边：形成循环
  - from: analyze_positive
    to: analyze_positive       # 自循环

loops:
  - node: analyze_positive
    max_iterations: 5
    exit_condition: confidence_reached
```

### 3.2 节点注册表（Registry）

配置文件中的 `type`、`condition`、`merge`、`exit_condition` 都是**名称引用**，对应的实现在代码中通过 Registry 注册：

```go
// Registry 管理所有可被配置文件引用的命名组件
type Registry[S any] struct {
    nodes      map[string]NodeFunc[S]
    conditions map[string]func(ctx context.Context, state S) bool
    merges     map[string]MergeFunc[S]
    exits      map[string]func(ctx context.Context, state S) bool
}

func NewRegistry[S any]() *Registry[S]

// 注册节点实现
func (r *Registry[S]) RegisterNode(typeName string, fn NodeFunc[S])

// 注册条件判断函数（配置中 condition 引用）
func (r *Registry[S]) RegisterCondition(name string, fn func(ctx context.Context, state S) bool)

// 注册合并函数（配置中 merge 引用）
func (r *Registry[S]) RegisterMerge(name string, fn MergeFunc[S])

// 注册循环退出条件（配置中 exit_condition 引用）
func (r *Registry[S]) RegisterExitCondition(name string, fn func(ctx context.Context, state S) bool)
```

### 3.3 加载与构建

```go
// 方式一：从配置文件加载
registry := gf.NewRegistry[TextState]()
registry.RegisterNode("tokenizer", tokenizeFunc)
registry.RegisterNode("sentiment_analyzer", analyzeFunc)
registry.RegisterNode("summarizer", summarizeFunc)
registry.RegisterCondition("score_check", func(ctx context.Context, s TextState) bool {
    return s.Score > 0.5
})
registry.RegisterMerge("result_merger", func(ctx context.Context, parent TextState, branches []TextState) (TextState, error) {
    // 合并逻辑
    return merged, nil
})
registry.RegisterExitCondition("confidence_reached", func(ctx context.Context, s TextState) bool {
    return s.Confidence < 0.95  // 达到 0.95 则退出循环
})

graph, err := gf.LoadFromFile[TextState]("workflow.yaml", registry)
if err != nil {
    panic(err)
}

// 方式二：编程式构建（与之前一样）
g := gf.NewGraph[MyState]("dynamic-graph")
g.AddNode("A", nodeAFunc)
// ...

// 方式三：混合模式 —— 从文件加载基本结构，再程序化扩展
graph, _ := gf.LoadFromFile[TextState]("base.yaml", registry)
graph.AddNode("extra_node", extraFunc)       // 追加节点
graph.AddEdge("summarize", "extra_node")      // 追加边
graph.Compile()
```

### 3.4 配置 vs 代码的职责边界

```
配置文件负责（What）           代码负责（How）
─────────────────────────    ─────────────────────────
 节点的名称和编排顺序           节点的具体实现逻辑
 边的连接关系                  条件判断的具体逻辑
 条件分支的路由规则             合并函数的处理细节
 并行/扇入的拓扑结构           状态类型的定义
 循环的最大迭代次数             循环退出条件的判断
 超时和重试策略的参数
```

这样的分离意味着：**修改图结构不需要重新编译**。只需要修改 YAML 文件并重新加载即可。这对于 A/B 测试不同编排策略、快速调整 Agent 行为、以及后续的可视化编辑器都是友好的。

### 3.5 配置文件模块化

对于复杂图，支持将配置拆分到多个文件：

```yaml
# main.yaml
name: complex-pipeline
entry: router

include:
  - nodes/preprocessing.yaml
  - nodes/analysis.yaml
  - nodes/output.yaml

nodes:
  - name: router
    type: router
    # ...

edges:
  # 跨文件边引用同样有效
  - from: router
    to: normalize   # 定义在 preprocessing.yaml 中
```

```yaml
# nodes/preprocessing.yaml
nodes:
  - name: normalize
    type: text_normalizer
  - name: tokenize
    type: tokenizer
```

### 3.6 配置结构定义（Go 类型）

```go
// Config 是 YAML 文件的顶层结构
type Config struct {
    Name    string   `yaml:"name"`
    Version string   `yaml:"version"`
    Entry   string   `yaml:"entry"`
    Include []string `yaml:"include,omitempty"`
    Nodes   []NodeDef   `yaml:"nodes"`
    Edges   []EdgeDef   `yaml:"edges"`
    Loops   []LoopDef   `yaml:"loops,omitempty"`
}

type NodeDef struct {
    Name    string       `yaml:"name"`
    Type    string       `yaml:"type"`             // 对应 Registry 中的节点类型
    Timeout string       `yaml:"timeout,omitempty"` // "30s", "1m"
    Retry   *RetryDef    `yaml:"retry,omitempty"`
}

type RetryDef struct {
    MaxAttempts int    `yaml:"max_attempts"`
    Backoff     string `yaml:"backoff,omitempty"`
    MaxBackoff  string `yaml:"max_backoff,omitempty"`
}

type EdgeDef struct {
    From      OneOrMany `yaml:"from"`
    To        OneOrMany `yaml:"to,omitempty"`
    Condition string    `yaml:"condition,omitempty"`
    Branches  []Branch  `yaml:"branches,omitempty"`
    Merge     string    `yaml:"merge,omitempty"`
}

type Branch struct {
    When string `yaml:"when"`   // 条件分支名，"default" 表示兜底
    To   string `yaml:"to"`
}

type LoopDef struct {
    Node          string `yaml:"node"`
    MaxIterations int    `yaml:"max_iterations,omitempty"`
    ExitCondition string `yaml:"exit_condition,omitempty"`
}

// OneOrMany 支持字符串或字符串数组，YAML 反序列化时处理
type OneOrMany []string
```

### 3.7 配置验证

`LoadFromFile` 内部执行与 `Compile()` 相同的静态验证：

| 检查项 | 错误级别 |
|--------|---------|
| 入口节点未定义 | Error |
| 边引用的节点不存在 | Error |
| 条件边引用的 condition 未注册 | Error |
| 合并引用的 merge 未注册 | Error |
| 节点 type 未注册 | Error |
| 节点存在但不可达 | Warning |
| 循环节点缺少 max_iterations 和 exit_condition | Warning |

---

## 4. 执行模型

### 4.1 整体模型：Pregel 风格的超级步（Superstep）

借鉴 Google Pregel 图计算模型：

```
1. 初始化：队列 = [入口节点]，已完成 = {}
2. 循环（每个超级步）：
   a. 选出队列中所有"就绪"的节点（入度为 0 或在当前步所有前驱已完成）
   b. 将就绪节点分组：
      - 互相独立的节点 → 可并行执行
      - 共享汇聚目标的节点 → 顺序执行（合并时需等齐）
   c. 执行该组节点（并行或串行）
   d. 对每个完成的节点：
      - 检查是否有条件边 → 评估条件，选出下一个节点
      - 检查是否有普通边 → 将目标加入队列
      - 检查是否有回边 → 检查迭代次数和退出条件
   e. 若有 checkpoint 配置 → 保存当前状态
   f. 若队列为空 → 执行结束
3. 返回最终状态
```

### 4.2 顺序执行

```
A -> B -> C

Superstep 1: 执行 A，队列变为 [B]
Superstep 2: 执行 B，队列变为 [C]
Superstep 3: 执行 C，队列变为 []
```

### 4.3 条件分支

```
A -> B (if score > 0.5)
A -> C (default)

Superstep 1: 执行 A，评估条件，队列变为 [B] 或 [C]
```

- 条件按定义顺序求值，第一个匹配的生效
- 最后一个条件可以 `If` 为 nil，表示兜底默认路径
- 如果一个节点同时有普通边和条件边：条件边优先；条件都不匹配 → 走普通边

### 4.4 并行扇出 / 扇入

```
        -> B -
A -           -> D
        -> C -

Superstep 1: 执行 A，发现 A 有两条出边 → 队列 = [B, C]
Superstep 2: B 和 C 并行执行（goroutine）
             - 各自收到 A 状态的深拷贝
             - 等待两者都完成
             - B 和 C 的出口都是 D → 调用 MergeFunc 合并状态
Superstep 3: 执行 D（使用合并后的状态）
```

**并行错误处理：** 任一分支失败 → 通过 `context.WithCancel` 取消其他分支 → 返回错误。

### 4.5 循环

```
A -> B -> C -> A (回边)

Superstep N:   执行 A，队列 = [B]
Superstep N+1: 执行 B，队列 = [C]
Superstep N+2: 执行 C：
  - 检查回边 C->A
  - increment iterCount[A]
  - 若 iterCount[A] < maxIterations && loopExit 条件为 true
    → A 重新入队
  - 否则 → 循环终止
```

**安全机制：**

- 每个循环入口节点有 `maxIterations` 上限（默认 1000），防止死循环
- 可选的 `loopExit` 函数：`func(state S) bool`，返回 false 则退出循环
- 循环检测在 `Compile()` 阶段通过 DFS 完成，回边被标记但允许存在

### 4.6 终止条件

图执行终止当：
1. 队列为空（没有更多节点要执行）
2. 上下文被取消（外部超时或并行分支失败）
3. 任何线程返回未处理错误（非重试策略覆盖的错误）

---

## 5. 流式处理模型

流式处理是可选的、显式的能力，核心思想：**流通道嵌入在状态中**。

```go
type MyState struct {
    Text     string
    Tokens   chan string   // 产生流的节点向这里写
    Results  []string      // 消费流的节点从这里读
}
```

### 框架提供的流工具

```go
// Stream 是泛型、可关闭的通道包装
type Stream[T any] struct {
    ch   chan T
    done chan struct{}
    err  error
}

func (s *Stream[T]) Send(v T) bool   // 非阻塞发送
func (s *Stream[T]) Chan() <-chan T  // 只读通道
func (s *Stream[T]) Close()          // 关闭
func (s *Stream[T]) CloseWithError(error)

// 工具函数
func Merge[T any](ctx context.Context, streams ...<-chan T) *Stream[T]
func Broadcast[T any](ctx context.Context, in <-chan T, n int) []*Stream[T]
```

**设计选择：** 不采用类似 Eino 的框架级自动流处理。显式流的优点是调试简单、行为可预期。后续 AI Agent 层可以封装自动流以提供更便捷的体验。

---

## 6. 持久化与恢复

### 6.1 Checkpoint 结构

```go
type Checkpoint struct {
    ID        string            // UUID
    GraphName string
    State     []byte            // 序列化的状态
    StateType string            // 类型名，用于恢复时校验
    Completed []string          // 已完成的节点名列表
    Queued    []string          // 当前队列中的节点
    IterCount map[string]int    // 循环迭代计数
    Timestamp time.Time
}
```

### 6.2 保存时机

| 时机 | 频率 |
|------|------|
| 每个节点执行后 | 默认（`CheckpointFreq=1`） |
| 每 N 个节点后 | 可配（`CheckpointFreq=N`） |
| 并行扇出前 | 始终保存（避免分支间不一致） |
| 重试前 | 始终保存（避免重复执行已成功的部分） |

### 6.3 存储接口

```go
type Manager interface {
    Save(ctx context.Context, cp *Checkpoint) error
    Load(ctx context.Context, id string) (*Checkpoint, error)
    List(ctx context.Context, graphName string) ([]Info, error)
    Delete(ctx context.Context, id string) error
    GetLatest(ctx context.Context, graphName string) (*Checkpoint, error)
}
```

内置实现：
- **InMemoryManager**：`sync.Map` 存储，适合测试和短期执行
- **FileManager**：目录 + JSON 文件存储，适合单机部署，无外部依赖

可扩展：RedisManager、SQLiteManager、etcdManager（放在子包中，避免核心引擎引入依赖）

### 6.4 恢复流程

```
1. 通过 ID 加载 Checkpoint
2. 校验 GraphName 和 StateType 是否匹配
3. 反序列化 State
4. 恢复 Completed、Queued、IterCount
5. 进入正常执行循环（已完成节点自动跳过）
```

---

## 7. 可观测性

### 7.1 Hook 接口

```go
type Hook[S any] interface {
    OnGraphStart(ctx context.Context, graphName string, state S)
    OnGraphEnd(ctx context.Context, graphName string, state S, err error)
    OnNodeStart(ctx context.Context, nodeName string, state S)
    OnNodeEnd(ctx context.Context, nodeName string, state S, err error, duration time.Duration)
    OnRetry(ctx context.Context, nodeName string, attempt int, lastErr error)
}
```

Hook 在引擎 goroutine 中同步调用。重量级操作（I/O、网络）应在 Hook 内部自行异步化。

多个 Hook 通过 `ComposeHooks` 组合。

### 7.2 OpenTelemetry 集成

内置 `OTelHook` 实现，自动创建：

| Span | 父 Span | 属性 |
|------|---------|------|
| `graphflow.graph.<name>` | 传入 ctx 的 span | graph.name |
| `graphflow.node.<name>` | 图 span | node.name, attempt |
| `graphflow.checkpoint.save` | 图 span | checkpoint.id |
| `graphflow.checkpoint.load` | 图 span | checkpoint.id |

Metrics：
- `graphflow.node.duration_ms` (Histogram) — 节点执行耗时
- `graphflow.node.executions` (Counter) — 节点执行次数，按 status 分
- `graphflow.graph.duration_ms` (Histogram) — 全图执行耗时

### 7.3 图可视化

```go
func (g *Graph[S]) DOT() string  // 输出 Graphviz DOT 格式
```

条件边渲染为带标签的分支，回边以虚线表示。

---

## 8. 模块结构

```
graphflow/
├── go.mod                    # module github.com/wzhongyou/graphflow
│
├── graph.go                  # Graph[S], Node[S], Edge, Condition[S], MergeFunc[S]
├── config.go                 # Config 结构体、YAML 解析、LoadFromFile
├── registry.go               # Registry[S]：命名组件注册表
├── engine.go                 # 引擎核心：顺序执行 + 条件路由
├── engine_parallel.go        # 并行扇出/扇入
├── engine_loop.go            # 循环检测与迭代控制
├── stream.go                 # Stream[T] 类型与工具函数
├── result.go                 # ExecutionResult[S] 结构体
│
├── hooks.go                  # Hook[S] 接口 + 组合 Hook
├── otel.go                   # OTelHook[S] 实现
│
├── checkpoint/
│   ├── types.go              # Checkpoint 结构 + Manager 接口
│   ├── memory.go             # InMemoryManager
│   └── file.go               # FileManager
│
├── agent/                    # Agent 层（独立包）
│   ├── state.go              # MessageState, Message, ToolCall
│   ├── nodes.go              # 预置节点：LLMNode, ToolNode, VectorRetrieveNode, HumanInputNode
│   ├── tools.go              # Tool 接口 + ToolRegistry
│   ├── agents.go             # Agent 模式：ReAct, RAG, Supervisor
│   ├── memory.go             # 短期/长期内存管理
│   └── llm.go                # LLM 接口定义（依赖注入）
│
├── examples/
│   ├── 01_sequential/        # 3 节点线性管道（YAML 配置 + 代码注册）
│   ├── 02_conditional/       # 条件分支路由
│   ├── 03_parallel/          # 扇出/扇入
│   ├── 04_loop/              # 自循环 + 退出条件
│   ├── 05_streaming/         # 流式节点
│   ├── 06_checkpoint/        # 持久化与恢复
│   ├── 07_http_server/       # HTTP 服务示例（展示如何作为 Server）
│   ├── 08_simple_chat/       # 简单聊天 Agent
│   ├── 09_rag_agent/         # RAG Agent
│   ├── 10_react_agent/       # ReAct Agent
│   └── 11_supervisor_agent/  # 多 Agent 协作
│
└── docs/
    ├── ai-agent-frameworks-survey.md   # AI Agent 框架调研
    ├── graphflow-design.md             # 本文档
    ├── core-guide.md                   # Core 层使用指南
    └── agent-guide.md                  # Agent 层使用指南
```

**包组织说明：**

- **根包** (`graphflow`)：Core 层，用户 `import "github.com/wzhongyou/graphflow"` 即可获得所有核心 API
- **agent 包** (`graphflow/agent`)：Agent 层，用户按需 `import "github.com/wzhongyou/graphflow/agent"`
- **checkpoint 子包**：独立子包，因为它有自己的持久化关注点，与核心引擎解耦
- **examples**：同时包含 Core 层和 Agent 层的示例

---

## 9. API 草图

### 9.1 配置文件方式（推荐）

```yaml
# workflow.yaml
name: text-pipeline
version: "1"
entry: to_upper

nodes:
  - name: to_upper
    type: text_transformer
  - name: reverse
    type: text_reverser
  - name: finalize
    type: text_finalizer

edges:
  - from: to_upper
    to: reverse
  - from: reverse
    to: finalize
```

```go
// 代码侧：注册节点实现
registry := gf.NewRegistry[TextState]()
registry.RegisterNode("text_transformer", toUpperFunc)
registry.RegisterNode("text_reverser", reverseFunc)
registry.RegisterNode("text_finalizer", finalizeFunc)

// 从配置文件加载
graph, err := gf.LoadFromFile[TextState]("workflow.yaml", registry)
if err != nil {
    panic(err)
}
// LoadFromFile 内部已调用 Compile()，无需再手动编译
```

完整示例（条件 + 并行 + 循环）：

```yaml
# advanced.yaml
name: analysis-pipeline
version: "1"
entry: classify

nodes:
  - name: classify
    type: classifier
  - name: analyze_positive
    type: analyzer
    retry:
      max_attempts: 3
      backoff: 2s
  - name: analyze_negative
    type: analyzer
  - name: summarize
    type: summarizer

edges:
  - from: classify
    condition: score_check
    branches:
      - when: positive
        to: analyze_positive
      - when: negative
        to: analyze_negative
  - from:
      - analyze_positive
      - analyze_negative
    to: summarize
    merge: result_merger

loops:
  - node: analyze_positive
    max_iterations: 5
    exit_condition: confidence_check
```

```go
registry := gf.NewRegistry[AnalysisState]()
registry.RegisterNode("classifier", classifyFunc)
registry.RegisterNode("analyzer", analyzeFunc)
registry.RegisterNode("summarizer", summarizeFunc)
registry.RegisterCondition("score_check", scoreCheckFunc)
registry.RegisterMerge("result_merger", mergeFunc)
registry.RegisterExitCondition("confidence_check", confidenceCheckFunc)

graph, _ := gf.LoadFromFile[AnalysisState]("advanced.yaml", registry)

engine := gf.NewEngine(graph)
result, _ := engine.Run(ctx, initialData,
    gf.WithHook(otelHook),
    gf.WithCheckpoint(checkpoint.NewFileManager("/tmp/checkpoints")),
)
```

### 9.2 编程式构建（动态场景）

```go
g := gf.NewGraph[MyState]("dynamic-graph")

g.AddNode("A", func(ctx context.Context, s MyState) (MyState, error) {
    return s, nil
})

g.SetEntryPoint("A")
g.AddEdge("A", "B")

// 条件分支
g.AddCondition("A", gf.Condition[MyState]{
    If:     func(ctx context.Context, s MyState) bool { return s.Score > 0.5 },
    Target: "B",
})
g.AddCondition("A", gf.Condition[MyState]{
    Target: "C", // If 为 nil = 兜底
})

// 汇聚合并
g.SetMergeFunc("D", func(ctx context.Context, parent MyState, branches []MyState) (MyState, error) {
    return merged, nil
})

// 循环
g.SetMaxIterations("A", 100)
g.SetLoopExit("A", func(ctx context.Context, s MyState) bool {
    return s.Count < 10
})

if err := g.Compile(); err != nil {
    panic(err)
}
```

### 9.3 混合模式

```go
// 从文件加载基础结构
graph, _ := gf.LoadFromFile[MyState]("base.yaml", registry)

// 程序化扩展
graph.AddNode("extra_validation", validateFunc)
graph.AddEdge("summarize", "extra_validation")
graph.Compile() // 重新编译验证
```

### 9.4 执行与恢复

```go
engine := gf.NewEngine(graph)

// 正常执行
result, err := engine.Run(ctx, initialState,
    gf.WithHook(otelHook),
    gf.WithCheckpoint(store),
    gf.WithCheckpointFreq(1),
)

// 从 checkpoint 恢复
result, err := engine.Run(ctx, nil,  // nil state → 使用 checkpoint 中的状态
    gf.WithResumeFrom(checkpointID),
)
```

---

## 10. 实现阶段规划

### Core 层实施阶段

| 阶段 | 内容 | 产出 |
|------|------|------|
| **P1** | 图模型 + 顺序执行 + 配置文件 | `graph.go`, `config.go`, `registry.go`, `engine.go`，YAML 定义 + 代码注册可运行 |
| **P2** | 条件路由 | 基于状态值的分支选择 |
| **P3** | 并行扇出/扇入 | goroutine 并发 + MergeFunc |
| **P4** | 循环 | 回边检测 + 迭代控制 + 退出条件 |
| **P5** | 流式处理 | Stream[T] 类型与工具函数 |
| **P6** | Checkpoint/恢复 | Manager 接口 + FileManager + InMemoryManager |
| **P7** | 可观测性 | Hook 接口 + OTelHook + DOT 输出 |
| **P8** | 结果封装 | ExecutionResult[S] 结构体，统一返回格式 |

### Agent 层实施阶段（Core 层完成 P1-P4 后开始）

| 阶段 | 内容 | 产出 |
|------|------|------|
| **A1** | Agent 状态抽象 | `agent/state.go`，MessageState, Message, ToolCall |
| **A2** | LLM 接口定义 | `agent/llm.go`，统一的 LLM 调用接口（支持 OpenAI、Anthropic、Gemini） |
| **A3** | 预置节点 | `agent/nodes.go`，LLMNode, ToolNode, VectorRetrieveNode, HumanInputNode |
| **A4** | 工具系统 | `agent/tools.go`，Tool 接口 + ToolRegistry + 内置工具（HTTP、DB、Calculator） |
| **A5** | 内存管理 | `agent/memory.go`，ShortTermMemory, LongTermMemory |
| **A6** | Agent 模式 - ReAct | `agent/agents.go`，ReAct Agent 模式 |
| **A7** | Agent 模式 - RAG | `agent/agents.go`，RAG Agent 模式 |
| **A8** | Agent 模式 - Supervisor | `agent/agents.go`，多 Agent 协作模式 |
| **A9** | 流式 Agent | `agent/stream.go`，流式 LLM 节点 + SSE 响应 |
| **A10** | Structured Output | `agent/structured.go`，强制 LLM 输出符合 JSON Schema 的结构体，含自动重试和解析 |
| **A11** | Prompt Template | `agent/prompt.go`，变量插值、few-shot 示例管理、chat/completion 两种格式 |
| **A12** | Summary Memory | `agent/memory.go` 扩展，超出窗口时调 LLM 将旧消息压缩为摘要注入上下文 |
| **A13** | RAG 文档管道 | `agent/rag/`，Document Loader（PDF/HTML/Markdown/数据库）、Chunking 策略（固定/语义切分）、Hybrid 检索（Dense + Sparse + Rerank） |
| **A14** | 代码执行沙箱 | `agent/tools/sandbox/`，Tool 级进程隔离（Docker / WASM），防止 LLM 生成代码直接在宿主执行 |
| **A15** | Guardrails | `agent/guardrails/`，输入/输出过滤器接口、PII 检测、敏感内容校验，可插拔在任意节点前后 |
| **A16** | 图级事件流 | `graph/engine.go` 扩展，`RunWithEvents` 返回节点级/token 级事件流，对外支持 SSE 透传 |
| **A17** | MCP 协议支持 | `agent/mcp/`，MCP Client（接入外部工具服务器）+ MCP Server（将本地 ToolRegistry 暴露为 MCP 服务） |
| **A18** | A2A 协议支持 | `agent/a2a/`，Agent-to-Agent 跨语言协作，兼容 Google ADK A2A 规范，支持异构 Agent 编排 |

### 文档和示例

| 阶段 | 内容 | 产出 |
|------|------|------|
| **D1** | README | 项目介绍、快速开始、核心概念 |
| **D2** | Core 使用指南 | `docs/core-guide.md`，Core 层完整文档 |
| **D3** | Agent 使用指南 | `docs/agent-guide.md`，Agent 层完整文档 |
| **D4** | Core 示例 | `examples/01-07`，Core 层 7 个示例 |
| **D5** | Agent 示例 | `examples/08-11`，Agent 层 4 个示例 |
| **D6** | 最佳实践 | 部署指南、性能优化、故障排查 |

### 优先级说明

**MVP（最小可用版本）**：Core 层 P1-P7 + Agent 层 A1-A4 + 文档 D1-D2-D4

**生产就绪版本**：Core 层 P1-P8 + Agent 层 A1-A16 + 文档 D1-D6 + 完整示例

**协议互操作版本**：在生产就绪基础上加 A17（MCP）+ A18（A2A）|

---

## 11. 边界情况与决策记录

### 已确认决策

| 决策 | 选择 | 原因 |
|------|------|------|
| 配置文件格式 | **YAML** | 嵌套数组表达自然，云原生生态通用，目标用户熟悉 |

### 11.1 空图

`Compile()` 返回 error，不允许空图执行。

### 11.2 不可达节点

`Compile()` 输出 warning（非 error），因为不可达节点可能是预留给未来使用的。

### 11.3 并行分支失败

通过 `context.WithCancel` 取消兄弟分支。返回第一个错误（或包装为 multierror）。

### 11.4 循环死循环

两层保护：(1) `maxIterations` 上限（默认 1000）；(2) 用户提供的 `loopExit` 条件。两者均不满足时触发 error。

### 11.5 状态 nil 返回

若节点返回 `nil` state（零值 + nil error），引擎视为 no-op，使用输入状态继续。

### 11.6 节点超时

每个节点通过 `context.WithTimeout` 包裹独立的超时控制，超时返回 `context.DeadlineExceeded`。

### 11.7 并发 Checkpoint 写入

Manager 实现必须线程安全。引擎可能在并行分支完成后并发调用 `Save`。

---

## 12. 与后续 AI Agent 层的关系

### 12.1 分层架构

当前设计的核心层（graphflow）保持 AI-无关。AI Agent 层作为独立包（graphflow/agent）在此基础上展开：

```
┌─────────────────────────────────────────────┐
│  应用层（用户代码）                           │
│  - 业务流程：订单处理、数据清洗               │
│  - Agent 应用：RAG、多 Agent 协作             │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│  Agent Layer (graphflow/agent 包)            │
│  - MessageState 状态抽象                      │
│  - 预置节点：LLMNode, ToolNode               │
│  - Agent 模式：ReAct, RAG, Supervisor         │
│  - 工具系统：ToolRegistry                     │
│  - 内存管理：Short/LongTermMemory            │
└─────────────────────────────────────────────┘
                    ↓ 依赖
┌─────────────────────────────────────────────┐
│  Core Layer (graphflow 包)                   │
│  - Graph[S], Engine                         │
│  - 并行/循环/条件路由                          │
│  - Checkpoint, Hook, OTel                   │
│  - Stream[T], ExecutionResult               │
└─────────────────────────────────────────────┘
```

### 12.2 Agent 层核心抽象

#### 状态抽象

```go
package agent

type Role string

const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleSystem    Role = "system"
    RoleTool      Role = "tool"
)

type Message struct {
    ID        string                 `json:"id"`
    Role      Role                   `json:"role"`
    Content   string                 `json:"content"`
    ToolCalls []ToolCall             `json:"tool_calls,omitempty"`
    ToolName  string                 `json:"tool_name,omitempty"`
    Metadata  map[string]any         `json:"metadata,omitempty"`
    Timestamp time.Time              `json:"timestamp"`
}

type ToolCall struct {
    ID        string                 `json:"id"`
    Name      string                 `json:"name"`
    Arguments map[string]any        `json:"arguments"`
    Result    string                 `json:"result,omitempty"`
}

type MessageState struct {
    Messages        []Message              `json:"messages"`
    Context         map[string]any         `json:"context"`
    CurrentAgent    string                 `json:"current_agent"`
    CompletedAgents []string              `json:"completed_agents"`
    NextAgent       string                 `json:"next_agent"`
    StepCount       int                    `json:"step_count"`
    MaxSteps        int                    `json:"max_steps"`
    Metadata        map[string]any         `json:"metadata"`
}
```

#### 预置节点

```go
// LLM 调用节点
type LLMNode struct {
    Model     LLMModel
    Prompt    string
    Stream    bool
}

// 工具调用节点
type ToolNode struct {
    Tools []Tool
}

// 向量检索节点
type VectorRetrieveNode struct {
    Embedder    Embedder
    VectorStore VectorStore
    TopK        int
}

// Human 输入节点（HITL）
type HumanInputNode struct {
    Prompt  string
    Timeout time.Duration
}
```

#### 工具系统

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() jsonschema.Schema
    Execute(ctx context.Context, args map[string]any) (string, error)
}

type ToolRegistry struct {
    tools map[string]Tool
}

func (r *ToolRegistry) Register(tool Tool)
func (r *ToolRegistry) Get(name string) (Tool, bool)
func (r *ToolRegistry) List() []Tool
```

#### Agent 模式

```go
// ReAct Agent
type ReActAgent struct {
    LLM      LLMModel
    Tools    []Tool
    MaxSteps int
}

func (a *ReActAgent) BuildGraph() (*gf.Graph[*MessageState], error)

// RAG Agent
type RAGAgent struct {
    LLM         LLMModel
    Embedder    Embedder
    VectorStore VectorStore
    TopK        int
}

func (a *RAGAgent) BuildGraph() (*gf.Graph[*MessageState], error)

// Supervisor Agent
type SupervisorAgent struct {
    LLM       LLMModel
    SubAgents map[string]Agent
    MaxRounds int
}

func (a *SupervisorAgent) BuildGraph() (*gf.Graph[*MessageState], error)
```

### 12.3 使用示例

#### 简单聊天 Agent

```go
import (
    "github.com/wzhongyou/graphflow"
    "github.com/wzhongyou/graphflow/agent"
)

func main() {
    // 配置 LLM
    llm := openai.NewChatModel("gpt-4")

    // 创建注册表
    registry := gf.NewRegistry[*agent.MessageState]()

    // 注册 LLM 节点
    registry.RegisterNode("chat", agent.NewLLMNode(agent.LLMNodeConfig{
        Model:  llm,
        Prompt: "你是一个有帮助的助手。",
    }))

    // 构建图
    graph := gf.NewGraph[*agent.MessageState]("simple-chat")
    graph.SetEntryPoint("chat")

    if err := graph.Compile(); err != nil {
        panic(err)
    }

    // 执行
    engine := gf.NewEngine(graph)

    result, err := engine.Run(ctx, &agent.MessageState{
        Messages: []agent.Message{
            {Role: agent.RoleUser, Content: "你好！"},
        },
    })

    fmt.Println(result.Messages[len(result.Messages)-1].Content)
}
```

#### RAG Agent

```go
func main() {
    // 使用预置的 RAG Agent
    ragAgent := agent.NewRAGAgent(agent.RAGAgentConfig{
        LLM:         openai.NewChatModel("gpt-4"),
        Embedder:    openai.NewEmbedder(),
        VectorStore: pgvector.NewStore(),
        TopK:        5,
    })

    graph, _ := ragAgent.BuildGraph()
    engine := gf.NewEngine(graph)

    result, _ := engine.Run(ctx, &agent.MessageState{
        Messages: []agent.Message{
            {Role: agent.RoleUser, Content: "如何使用 Graphflow？"},
        },
    })

    fmt.Println(result.Messages[len(result.Messages)-1].Content)
}
```

### 12.4 部署模式

#### 作为库嵌入服务（推荐）

```go
// main.go
package main

import (
    "net/http"
    "github.com/wzhongyou/graphflow"
    "github.com/wzhongyou/graphflow/agent"
)

func main() {
    // 加载 Agent 图
    ragAgent := agent.NewRAGAgent(...)
    graph, _ := ragAgent.BuildGraph()
    engine := gf.NewEngine(graph)

    // 暴露 HTTP 接口
    http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
        var req ChatRequest
        json.NewDecoder(r.Body).Decode(&req)

        result, err := engine.Run(r.Context(), &agent.MessageState{
            Messages: []agent.Message{
                {Role: agent.RoleUser, Content: req.Message},
            },
        })

        json.NewEncoder(w).Encode(map[string]any{
            "response": result.Messages[len(result.Messages)-1].Content,
            "error":    err,
        })
    })

    http.ListenAndServe(":8080", nil)
}
```

部署：

```bash
# 编译
go build -o rag-service

# 运行
./rag-service
```

#### 通过示例代码展示 Server 模式

在 `examples/07_http_server` 中展示如何：
- 暴露为 HTTP 服务
- 支持流式响应（SSE）
- 集成 Checkpoint 恢复
- 添加认证和限流

**注意**：Graphflow 不提供正式的 Server 实现，只提供示例代码展示如何作为 Server 使用。

---

## 13. 结果封装与终止条件

### 13.1 ExecutionResult 结构

```go
type ExecutionResult[S any] struct {
    // 最终状态
    FinalState S `json:"final_state"`

    // 执行元信息
    GraphName   string    `json:"graph_name"`
    ExecutionID string    `json:"execution_id"`
    StartTime   time.Time `json:"start_time"`
    EndTime     time.Time `json:"end_time"`

    // 终止原因
    Termination TerminationReason `json:"termination"`
    Error       error             `json:"error,omitempty"`

    // 指标
    NodeCount    int           `json:"node_count"`
    TotalNodes   int           `json:"total_nodes"`
    TotalSteps   int           `json:"total_steps"`
    TotalDuration time.Duration `json:"total_duration"`

    // Checkpoint
    CheckpointID string `json:"checkpoint_id,omitempty"`

    // 追踪信息（如果启用 OTel）
    TraceID string `json:"trace_id,omitempty"`
    SpanID  string `json:"span_id,omitempty"`
}

type TerminationReason string

const (
    TerminationCompleted TerminationReason = "completed"  // 正常完成
    TerminationCancelled TerminationReason = "cancelled"  // 上下文取消
    TerminationError     TerminationReason = "error"      // 节点错误
    TerminationTimeout   TerminationReason = "timeout"    // 超时
    TerminationMaxSteps  TerminationReason = "max_steps"  // 达到最大步数
)
```

### 13.2 引擎 API 更新

```go
// Run 返回 ExecutionResult
func (e *Engine[S]) Run(ctx context.Context, initialState S, opts ...Option) (*ExecutionResult[S], error)

// RunStream 返回流式结果
func (e *Engine[S]) RunStream(ctx context.Context, initialState S, opts ...Option) (*StreamResult[S], error)

// RunWithEvents 返回事件流
func (e *Engine[S]) RunWithEvents(ctx context.Context, initialState S, opts ...Option) (*EventResult[S], error)
```

### 13.3 终止节点支持

```go
// 标记终止节点
func (g *Graph[S]) MarkTerminalNode(name string, reason TerminationReason)

// 示例
g.AddNode("success", successNode)
g.MarkTerminalNode("success", TerminationCompleted)

g.AddNode("failure", failureNode)
g.MarkTerminalNode("failure", TerminationError)
```

---

## 14. 部署模式

### 14.1 作为库嵌入（推荐）

Graphflow 是一个库，不提供独立的 Server。

**优点**：
- 性能最优：无网络开销、无序列化
- 灵活性高：可深度集成到现有系统
- 部署简单：单二进制部署
- 依赖管理：无外部服务依赖

**适用场景**：
- Go 微服务编排
- 内部工具链
- 高性能场景
- 单体应用内部工作流

**使用方式**：

```go
// main.go
package main

import (
    "net/http"
    "github.com/wzhongyou/graphflow"
)

func main() {
    // 加载图
    registry := gf.NewRegistry[OrderState]()
    // ... 注册节点
    graph, _ := gf.LoadFromFile[OrderState]("order.yaml", registry)
    engine := gf.NewEngine(graph)

    // 暴露 HTTP 接口
    http.HandleFunc("/api/orders/process", func(w http.ResponseWriter, r *http.Request) {
        var state OrderState
        json.NewDecoder(r.Body).Decode(&state)

        result, err := engine.Run(r.Context(), state,
            gf.WithHook(otelHook),
            gf.WithCheckpoint(checkpointStore),
        )

        json.NewEncoder(w).Encode(map[string]any{
            "result": result,
            "error":  err,
        })
    })

    http.ListenAndServe(":8080", nil)
}
```

**部署**：

```bash
# 编译
go build -o order-service

# 运行
./order-service
```

### 14.2 作为独立 Gateway（示例代码）

在 `examples/07_http_server` 中展示如何构建独立的图执行服务：

```go
// examples/07_http_server/main.go
package main

import (
    "github.com/wzhongyou/graphflow"
)

func main() {
    server := NewGraphflowServer()

    // 动态注册图（从数据库/配置中心）
    server.RegisterGraph("order-process", loadOrderGraph())
    server.RegisterGraph("rag-agent", loadRAGAgent())

    server.Listen(":8080")
}
```

**注意**：这只是示例代码，展示如何使用 Graphflow 构建 Server。Graphflow 本身不提供正式的 Server 实现。

### 14.3 部署选项

| 选项 | 说明 | 适用场景 |
|------|------|---------|
| **嵌入微服务** | Graphflow 作为库嵌入到现有服务 | Go 微服务、内部系统 |
| **独立 Gateway** | 使用示例代码构建独立服务 | 多语言架构、统一管理 |
| **Serverless** | 部署到 Cloud Run / Lambda | 云原生、按需扩展 |
| **K8s Deployment** | 通过 K8s 部署 | 企业级、高可用 |

### 14.4 与 LangChain/LangGraph 的对比

| 维度 | Graphflow | LangChain/LangGraph |
|------|-----------|---------------------|
| **部署模式** | 库模式 | 库模式 + Langserve（第三方） |
| **性能** | 最优（无网络开销） | 依赖网络序列化 |
| **灵活性** | 极高 | 中等 |
| **多语言支持** | 需要独立 Server | 通过 Langserve 支持 |
| **学习曲线** | Go 开发者友好 | Python 开发者友好 |

---

## 15. 待讨论的设计决策

### Core 层决策

1. **编译 vs 运行时验证**：`Compile()` 做静态验证（节点存在性、边引用、循环检测），但保留一些灵活性（如允许不可达节点）。是否还有其他需要在 `Compile()` 中检查的项？

2. **并行默认策略**：当一个节点有多条普通出边时，是默认并行执行还是串行执行？当前设计是按并行处理，因为这是更常见的高性能场景。

3. **状态粒度**：当前一个图只有一个状态类型 `S`。如果后续需要子图有独立的状态类型，是否需要支持？（类似 Eino 的 Workflow 模式，结构体字段级数据映射）

4. **错误恢复粒度**：当前 retry 是节点级别的。是否需要支持图级别的全局错误处理（如失败后从头重跑）？

5. **流式 + Checkpoint**：流式传输中的 checkpoint 语义是什么？是否需要等流消费完再 checkpoint？建议 P5/P6 阶段先标记为限制。

6. **条件表达式在配置中**：当前条件边通过 `condition` 名称引用代码中的函数。是否需要在配置中支持简单的表达式（如 `score > 0.5`），减少代码注册量？

### Agent 层决策

7. **LLM 接口设计**：✅ 已决策。支持流式调用（`ChatStream`）+ Function Calling（`ChatRequest.Tools`）。LLM 接入通过 llmgate 适配层注入，LLMModel 接口保持极简，provider 细节隔离在适配层内。

8. **MCP 协议支持**：计划作为 A17 阶段实现。MCP Client 接入外部工具服务器；MCP Server 将 ToolRegistry 中注册的工具暴露为标准 MCP 服务，方便与其他 AI 框架互操作。

9. **A2A 协议支持**：计划作为 A18 阶段实现，兼容 Google ADK A2A 规范，使 Graphflow Agent 可被其他语言的 Agent 框架调用。

10. **向量存储接口**：向量存储接口应该多通用？是否支持 pgvector、Milvus、Weaviate 等？

11. **Human-in-the-Loop 模式**：支持多少种 HITL 模式？（参考 Eino 的 5-8 种模式）

12. **Memory 持久化**：短期/长期内存如何持久化？是否需要 Redis/S3 支持？

### 架构决策

13. **Server 模式**：是否提供正式的 Server 实现？还是只提供示例代码？当前决策：只提供示例代码。

14. **模块化程度**：Agent 层是否应该进一步拆分？（如 `agent/llm`, `agent/tools`, `agent/memory` 独立包）

15. **测试策略**：如何测试 Agent 层？是否需要 Mock LLM？

### 产品决策

16. **MVP 范围**：MVP 应该包含哪些功能？Core 层 P1-P7？Agent 层 A1-A4？

17. **文档优先级**：哪些文档必须先完成？README、Core Guide、Agent Guide？

18. **示例优先级**：哪些示例必须先完成？顺序、条件、并行、循环、Checkpoint、简单 Agent？

### 性能决策

19. **并行度控制**：是否需要限制并行度？（避免 goroutine 泛滥）

20. **内存优化**：状态深拷贝是否可以优化？（使用指针、共享内存）

---

## 16. 参考实现对比

### 与 LangGraph 的对比

| 特性 | Graphflow | LangGraph |
|------|-----------|-----------|
| **语言** | Go | Python |
| **状态模型** | 泛型 S | StateGraph + Reducer |
| **配置方式** | YAML + 代码 | 纯代码 |
| **Checkpoint** | 接口 + 多实现 | 内置多种存储 |
| **可观测性** | Hook + OTel | LangSmith |
| **部署** | 嵌入式 | 嵌入式 + Langserve |
| **学习曲线** | 中等（Go 门槛） | 较高（LangChain 门槛） |
| **性能** | 最优 | 中等（Python） |

### 与 Eino 的对比

| 特性 | Graphflow | Eino |
|------|-----------|------|
| **语言** | Go | Go |
| **编排 API** | Graph | Graph + Chain + Workflow |
| **配置方式** | YAML + 代码 | 纯代码 |
| **HITL** | 计划中 | 支持 5-8 种模式 |
| **流式处理** | 显式流 | 框架级自动流 |
| **部署** | 嵌入式 | 嵌入式 + CloudWeGo |
| **生产验证** | 新项目 | 字节数百服务 |
| **AI 能力** | Agent 层计划中 | ADK 模块完善 |

### 与 Google ADK Go 的对比

| 特性 | Graphflow | Google ADK Go |
|------|-----------|---------------|
| **语言** | Go | Go |
| **配置方式** | YAML + 代码 | YAML |
| **A2A 支持** | 计划中 | 支持（Linux Foundation） |
| **云原生** | 通用 | Cloud Run 深度集成 |
| **OTel** | 内置 | 原生集成 |
| **Agent 模式** | 计划中 | 自愈、插件 |

---

## 17. 关键里程碑

### MVP 发布（预计 2026-06）

**Core 层**：
- ✅ 图模型 + 顺序执行
- ✅ 条件路由
- ✅ 并行扇出/扇入
- ✅ 循环控制
- ✅ Checkpoint
- ✅ Hook + OTel

**Agent 层**：
- ✅ MessageState 抽象
- ✅ LLM 接口
- ✅ 基础节点（LLM、Tool）
- ✅ 工具系统

**文档和示例**：
- ✅ README
- ✅ Core Guide
- ✅ 6 个 Core 示例
- ✅ 2 个 Agent 示例

### v0.2 发布（预计 2026-07）

**Agent 层增强**：
- ✅ RAG Agent 模式
- ✅ ReAct Agent 模式
- ✅ 内存管理
- ✅ 流式 LLM

**文档和示例**：
- ✅ Agent Guide
- ✅ 4 个 Agent 示例

### v0.3 发布（预计 2026-08）

**高级特性**：
- ✅ Supervisor Agent 模式
- ✅ Human-in-the-Loop
- ✅ 向量检索节点

**企业特性**：
- ✅ Redis Checkpoint
- ✅ 熔断器
- ✅ 限流

### v0.4 发布（预计 2026-11）

**Agent 能力补全**：
- ✅ Structured Output，LLM 强制输出 JSON Schema 结构体（A10）
- ✅ Prompt Template，变量插值 + few-shot 管理（A11）
- ✅ Summary Memory，窗口溢出时 LLM 压缩摘要（A12）
- ✅ RAG 文档管道，Loader / Chunker / Hybrid 检索（A13）
- ✅ 代码执行沙箱，Docker / WASM 进程隔离（A14）
- ✅ Guardrails，输入/输出过滤 + PII 检测（A15）
- ✅ 图级事件流，RunWithEvents / SSE 透传（A16）

### v1.0 发布（预计 2026-12）

**生产就绪**：
- ✅ 完整文档
- ✅ 性能优化
- ✅ 稳定性保证
- ✅ 社区生态

**协议互操作**：
- ✅ MCP Client / Server（A17）
- ✅ A2A 跨语言 Agent 协作（A18）

---

## 18. 开发指南

### 贡献流程

1. Fork 仓库
2. 创建特性分支（`git checkout -b feature/amazing-feature`）
3. 提交更改（`git commit -m 'Add amazing feature'`）
4. 推送到分支（`git push origin feature/amazing-feature`）
5. 创建 Pull Request

### 代码规范

- 遵循 `gofmt` 格式
- 遵循 `golint` 检查
- 必须有单元测试
- 复杂逻辑必须有注释
- 公开 API 必须有文档

### 测试要求

- 单元测试覆盖率 > 80%
- 集成测试覆盖主要场景
- 性能测试对比基准
- 并发测试（race detector）

---

## 19. 许可证

本项目采用 MIT 许可证。

---

## 20. 联系方式

- 作者：wangzhongyou
- 仓库：https://github.com/wzhongyou/graphflow
- 文档：https://github.com/wzhongyou/graphflow/docs
- 讨论：https://github.com/wzhongyou/graphflow/discussions
