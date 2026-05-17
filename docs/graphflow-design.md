# Graphflow Architecture Design Document

> Status: Draft, under discussion (v0.2 - Agent layer design added)

## 1. Positioning and Goals

**Graphflow** is a general-purpose workflow orchestration engine implemented in Go, with a layered architecture:

```
┌─────────────────────────────────────────────┐
│   Application Layer (user code)              │
│  - Business process orchestration            │
│  - AI Agent applications                     │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│  Agent Layer (graphflow/agent package)       │
│  - Built-in Agent nodes                      │
│  - Agent pattern implementations             │
│  - Tool system                               │
│  - Memory management                         │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│  Core Layer (graphflow package)              │
│  - General-purpose graph execution engine    │
│  - Config-driven + programmatic API          │
│  - Parallel / loop / conditional routing     │
│  - Checkpoint + Hook + OTel                  │
└─────────────────────────────────────────────┘
```

**Core Positioning**:

- **Core Layer**: Completely AI-agnostic graph execution engine, suitable for business orchestration, microservice orchestration, ETL, event-driven architectures, and more.
- **Agent Layer**: AI Agent abstraction layer built on top of the Core layer, providing LLM nodes, tool system, Agent patterns, etc.

**Design Principles**:

- **Layered Decoupling**: Core and Agent layers are loosely coupled; Agent depends on Core, but Core never depends on Agent.
- **Config-Driven**: Graph structure defined via YAML configuration files, decoupled from code; node implementations injected through a registry.
- **Type-Safe**: Leverages Go generics to ensure at compile time that all nodes in a graph operate on the same state type.
- **Minimal Dependencies**: Core layer has zero external dependencies; observability and persistence are pluggable via interfaces.
- **Progressive Complexity**: Simple scenarios (linear chains) are simple to write; complex scenarios (parallel/loop/conditional) are composable.
- **Single Binary Deployment**: No Python runtime, no external service dependency required to run.
- **Deployment Flexibility**: Embedded as a library in user services; no standalone server provided (example code shows how to use as a server).

**Target Users**:

1. **Business Development Teams**: Use the Core layer to orchestrate microservices, ETL, and event-driven workflows.
2. **AI Development Teams**: Use the Agent layer to build RAG, multi-agent collaboration, and HITL applications.
3. **Hybrid Scenarios**: Systems that need both business orchestration and Agent capabilities.

---

## 2. Core Abstractions

### 2.1 Node

A node is the smallest unit of execution in the graph, with a minimal signature:

```go
// NodeFunc receives state, returns updated state and optional error
type NodeFunc[S any] func(ctx context.Context, state S) (S, error)
```

All nodes share the same state type `S`, uniformly constrained by the graph's generic parameter. Each node sees the complete state and freely chooses which fields to read and write.

### 2.2 Edge

Edges define transition relationships between nodes, categorized into three types:

| Type | Semantics | Example |
|------|-----------|---------|
| **Normal Edge** | Unconditional transfer from A to B after A completes | `A -> B` |
| **Conditional Edge** | After A completes, select target based on state value | `A -> B(if x>0), C(else)` |
| **Back Edge** | Points back to an upstream node, forming a loop | `B -> A` (A is the loop entry) |

### 2.3 State

Fully user-defined; the engine has no knowledge of the state's internal structure. The engine optionally needs two capabilities:

```go
// Serialization: used for checkpointing, defaults to gob
type StateSerializer interface {
    MarshalState() ([]byte, error)
    UnmarshalState([]byte) error
}

// Deep Copy: used for parallel branch isolation, defaults to gob round-trip
type StateDeepCopier[S any] interface {
    DeepCopy() S
}
```

### 2.4 MergeFunc

When multiple parallel branches converge on the same node, a function is needed to merge their states into one:

```go
// parent: state snapshot at the fork point
// branches: states after each branch execution, in edge definition order
type MergeFunc[S any] func(ctx context.Context, parent S, branches []S) (S, error)
```

If the user does not provide a MergeFunc, the engine uses the default strategy: **overwrite in branch order; the last branch's state becomes the primary state**.

---

## 3. Config-Driven: Graph Definition

Graphflow provides two ways to define graphs, which can be mixed:

| Approach | Use Case | Advantage |
|----------|----------|-----------|
| **YAML Config File** | Static graphs, production deployment, team collaboration | Highly readable, clear graph structure, easy version management |
| **Programmatic API** | Dynamic graphs, testing, complex conditional logic | Flexible, can generate graph structures programmatically |

The relationship: **config files define the structure; code registers the implementations**. This follows the same philosophy as CI/CD pipelines (GitHub Actions YAML) and Google ADK Go's declarative configuration.

### 3.1 Configuration File Format (YAML)

```yaml
# workflow.yaml
name: text-analysis-pipeline
version: "1"
entry: tokenize

nodes:
  - name: tokenize
    type: tokenizer           # Corresponds to a node type registered in Registry
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
  # Normal edge: unconditional transfer
  - from: tokenize
    to: classify

  # Conditional edge: route based on condition function
  - from: classify
    condition: score_check
    branches:
      - when: positive
        to: analyze_positive
      - when: negative
        to: analyze_negative

  # Parallel fan-out (from has multiple targets, automatically parallel)
  - from: tokenize
    to:
      - analyze_positive
      - analyze_negative

  # Fan-in convergence (multiple from pointing to same to)
  - from:
      - analyze_positive
      - analyze_negative
    to: summarize
    merge: result_merger       # Corresponds to a merge function registered in Registry

  # Back edge: forms a loop
  - from: analyze_positive
    to: analyze_positive       # Self-loop

loops:
  - node: analyze_positive
    max_iterations: 5
    exit_condition: confidence_reached
```

### 3.2 Node Registry

The `type`, `condition`, `merge`, and `exit_condition` fields in config files are all **name references**; their corresponding implementations are registered in code through the Registry:

```go
// Registry manages all named components referenced by config files
type Registry[S any] struct {
    nodes      map[string]NodeFunc[S]
    conditions map[string]func(ctx context.Context, state S) bool
    merges     map[string]MergeFunc[S]
    exits      map[string]func(ctx context.Context, state S) bool
}

func NewRegistry[S any]() *Registry[S]

// Register a node implementation
func (r *Registry[S]) RegisterNode(typeName string, fn NodeFunc[S])

// Register a condition function (referenced by 'condition' in config)
func (r *Registry[S]) RegisterCondition(name string, fn func(ctx context.Context, state S) bool)

// Register a merge function (referenced by 'merge' in config)
func (r *Registry[S]) RegisterMerge(name string, fn MergeFunc[S])

// Register a loop exit condition (referenced by 'exit_condition' in config)
func (r *Registry[S]) RegisterExitCondition(name string, fn func(ctx context.Context, state S) bool)
```

### 3.3 Loading and Building

```go
// Approach 1: Load from config file
registry := gf.NewRegistry[TextState]()
registry.RegisterNode("tokenizer", tokenizeFunc)
registry.RegisterNode("sentiment_analyzer", analyzeFunc)
registry.RegisterNode("summarizer", summarizeFunc)
registry.RegisterCondition("score_check", func(ctx context.Context, s TextState) bool {
    return s.Score > 0.5
})
registry.RegisterMerge("result_merger", func(ctx context.Context, parent TextState, branches []TextState) (TextState, error) {
    // merge logic
    return merged, nil
})
registry.RegisterExitCondition("confidence_reached", func(ctx context.Context, s TextState) bool {
    return s.Confidence < 0.95  // exit loop when confidence reaches 0.95
})

graph, err := gf.LoadFromFile[TextState]("workflow.yaml", registry)
if err != nil {
    panic(err)
}

// Approach 2: Programmatic construction (same as before)
g := gf.NewGraph[MyState]("dynamic-graph")
g.AddNode("A", nodeAFunc)
// ...

// Approach 3: Hybrid — load base structure from file, extend programmatically
graph, _ := gf.LoadFromFile[TextState]("base.yaml", registry)
graph.AddNode("extra_node", extraFunc)       // append nodes
graph.AddEdge("summarize", "extra_node")      // append edges
graph.Compile()
```

### 3.4 Config vs. Code: Responsibility Boundary

```
Config is responsible for (What)     Code is responsible for (How)
─────────────────────────────────    ─────────────────────────────
 Node names and orchestration order   Node implementation logic
 Edge connections                     Conditional logic implementation
 Conditional branch routing           Merge function details
 Parallel / fan-in topology           State type definition
 Maximum loop iterations              Loop exit condition logic
 Timeout and retry parameters
```

This separation means: **changing the graph structure does not require recompilation**. Simply modify the YAML file and reload. This is friendly for A/B testing different orchestration strategies, quickly adjusting Agent behavior, and future visual editors.

### 3.5 Config File Modularization

For complex graphs, configuration can be split across multiple files:

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
  # Cross-file edge references are valid
  - from: router
    to: normalize   # defined in preprocessing.yaml
```

```yaml
# nodes/preprocessing.yaml
nodes:
  - name: normalize
    type: text_normalizer
  - name: tokenize
    type: tokenizer
```

### 3.6 Config Structure Definition (Go Types)

```go
// Config is the top-level structure of a YAML file
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
    Type    string       `yaml:"type"`             // Corresponds to node type in Registry
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
    When string `yaml:"when"`   // Condition branch name; "default" means fallback
    To   string `yaml:"to"`
}

type LoopDef struct {
    Node          string `yaml:"node"`
    MaxIterations int    `yaml:"max_iterations,omitempty"`
    ExitCondition string `yaml:"exit_condition,omitempty"`
}

// OneOrMany supports a single string or a string array, handled during YAML deserialization
type OneOrMany []string
```

### 3.7 Config Validation

`LoadFromFile` internally performs the same static validation as `Compile()`:

| Check | Severity |
|-------|----------|
| Entry node not defined | Error |
| Edge references non-existent node | Error |
| Conditional edge references unregistered condition | Error |
| Merge references unregistered merge function | Error |
| Node type not registered | Error |
| Node exists but is unreachable | Warning |
| Loop node missing max_iterations and exit_condition | Warning |

---

## 4. Execution Model

### 4.1 Overall Model: Pregel-Style Superstep

Inspired by Google's Pregel graph computing model:

```
1. Initialize: queue = [entry node], completed = {}
2. Loop (each superstep):
   a. Select all "ready" nodes from the queue (in-degree 0 or all predecessors completed in current step)
   b. Group ready nodes:
      - Mutually independent nodes → can execute in parallel
      - Nodes sharing a convergence target → execute sequentially (wait for all before merging)
   c. Execute the group of nodes (parallel or serial)
   d. For each completed node:
      - Check for conditional edges → evaluate conditions, select next node
      - Check for normal edges → add target to queue
      - Check for back edges → check iteration count and exit condition
   e. If checkpoint configured → save current state
   f. If queue is empty → execution ends
3. Return final state
```

### 4.2 Sequential Execution

```
A -> B -> C

Superstep 1: Execute A, queue becomes [B]
Superstep 2: Execute B, queue becomes [C]
Superstep 3: Execute C, queue becomes []
```

### 4.3 Conditional Branching

```
A -> B (if score > 0.5)
A -> C (default)

Superstep 1: Execute A, evaluate conditions, queue becomes [B] or [C]
```

- Conditions are evaluated in definition order; the first match wins.
- The last condition can have a nil `If`, representing the default fallback path.
- If a node has both normal and conditional edges: conditional edges take priority; if no condition matches → fall through to normal edges.

### 4.4 Parallel Fan-Out / Fan-In

```
        -> B -
A -           -> D
        -> C -

Superstep 1: Execute A, A has two outgoing edges → queue = [B, C]
Superstep 2: B and C execute in parallel (goroutines)
             - Each receives a deep copy of A's state
             - Wait for both to complete
             - Both B and C point to D → call MergeFunc to merge states
Superstep 3: Execute D (using merged state)
```

**Parallel error handling:** If any branch fails → cancel sibling branches via `context.WithCancel` → return error.

### 4.5 Loops

```
A -> B -> C -> A (back edge)

Superstep N:   Execute A, queue = [B]
Superstep N+1: Execute B, queue = [C]
Superstep N+2: Execute C:
  - Check back edge C->A
  - increment iterCount[A]
  - If iterCount[A] < maxIterations && loopExit condition is true
    → re-enqueue A
  - else → loop terminates
```

**Safety Mechanisms:**

- Each loop entry node has a `maxIterations` limit (default 1000) to prevent infinite loops.
- Optional `loopExit` function: `func(state S) bool`; returns false to exit the loop.
- Loop detection done at `Compile()` time via DFS; back edges are flagged but allowed.

### 4.6 Termination Conditions

Graph execution terminates when:
1. The queue is empty (no more nodes to execute)
2. The context is cancelled (external timeout or parallel branch failure)
3. Any goroutine returns an unhandled error (errors not covered by retry policy)

---

## 5. Streaming Processing Model

Streaming is an optional, explicit capability. The core idea: **stream channels are embedded within the state**.

```go
type MyState struct {
    Text     string
    Tokens   chan string   // Producing node writes here
    Results  []string      // Consuming node reads from here
}
```

### Framework-Provided Stream Utilities

```go
// Stream is a generic, closeable channel wrapper
type Stream[T any] struct {
    ch   chan T
    done chan struct{}
    err  error
}

func (s *Stream[T]) Send(v T) bool   // Non-blocking send
func (s *Stream[T]) Chan() <-chan T  // Read-only channel
func (s *Stream[T]) Close()          // Close
func (s *Stream[T]) CloseWithError(error)

// Utility functions
func Merge[T any](ctx context.Context, streams ...<-chan T) *Stream[T]
func Broadcast[T any](ctx context.Context, in <-chan T, n int) []*Stream[T]
```

**Design choice:** Not adopting framework-level automatic stream processing like Eino. The advantage of explicit streams is simpler debugging and predictable behavior. The future AI Agent layer can encapsulate automatic streaming for a more convenient experience.

---

## 6. Persistence and Recovery

### 6.1 Checkpoint Structure

```go
type Checkpoint struct {
    ID        string            // UUID
    GraphName string
    State     []byte            // Serialized state
    StateType string            // Type name, used for validation during recovery
    Completed []string          // List of completed node names
    Queued    []string          // Current queue of nodes
    IterCount map[string]int    // Loop iteration counts
    Timestamp time.Time
}
```

### 6.2 Save Timing

| Timing | Frequency |
|--------|-----------|
| After each node execution | Default (`CheckpointFreq=1`) |
| Every N nodes | Configurable (`CheckpointFreq=N`) |
| Before parallel fan-out | Always save (avoid inconsistency between branches) |
| Before retry | Always save (avoid re-executing already-successful parts) |

### 6.3 Storage Interface

```go
type Manager interface {
    Save(ctx context.Context, cp *Checkpoint) error
    Load(ctx context.Context, id string) (*Checkpoint, error)
    List(ctx context.Context, graphName string) ([]Info, error)
    Delete(ctx context.Context, id string) error
    GetLatest(ctx context.Context, graphName string) (*Checkpoint, error)
}
```

Built-in implementations:
- **InMemoryManager**: `sync.Map` storage, suitable for testing and short-lived executions.
- **FileManager**: Directory + JSON file storage, suitable for single-machine deployment, no external dependencies.

Extensible: RedisManager, SQLiteManager, etcdManager (placed in sub-packages to avoid introducing dependencies in the core engine).

### 6.4 Recovery Flow

```
1. Load Checkpoint by ID
2. Validate that GraphName and StateType match
3. Deserialize State
4. Restore Completed, Queued, IterCount
5. Enter normal execution loop (already-completed nodes are automatically skipped)
```

---

## 7. Observability

### 7.1 Hook Interface

```go
type Hook[S any] interface {
    OnGraphStart(ctx context.Context, graphName string, state S)
    OnGraphEnd(ctx context.Context, graphName string, state S, err error)
    OnNodeStart(ctx context.Context, nodeName string, state S)
    OnNodeEnd(ctx context.Context, nodeName string, state S, err error, duration time.Duration)
    OnRetry(ctx context.Context, nodeName string, attempt int, lastErr error)
}
```

Hooks are called synchronously in the engine goroutine. Heavy operations (I/O, network) should be async internally within the Hook.

Multiple Hooks are composed via `ComposeHooks`.

### 7.2 OpenTelemetry Integration

Built-in `OTelHook` implementation automatically creates:

| Span | Parent Span | Attributes |
|------|-------------|------------|
| `graphflow.graph.<name>` | Incoming ctx span | graph.name |
| `graphflow.node.<name>` | Graph span | node.name, attempt |
| `graphflow.checkpoint.save` | Graph span | checkpoint.id |
| `graphflow.checkpoint.load` | Graph span | checkpoint.id |

Metrics:
- `graphflow.node.duration_ms` (Histogram) — node execution duration
- `graphflow.node.executions` (Counter) — node execution count, grouped by status
- `graphflow.graph.duration_ms` (Histogram) — total graph execution duration

### 7.3 Graph Visualization

```go
func (g *Graph[S]) DOT() string  // Output Graphviz DOT format
```

Conditional edges are rendered as labeled branches; back edges are shown as dashed lines.

---

## 8. Module Structure

```
graphflow/
├── go.mod                    # module github.com/wzhongyou/graphflow
│
├── graph.go                  # Graph[S], Node[S], Edge, Condition[S], MergeFunc[S]
├── config.go                 # Config struct, YAML parsing, LoadFromFile
├── registry.go               # Registry[S]: named component registry
├── engine.go                 # Engine core: sequential execution + conditional routing
├── engine_parallel.go        # Parallel fan-out/fan-in
├── engine_loop.go            # Loop detection and iteration control
├── stream.go                 # Stream[T] type and utility functions
├── result.go                 # ExecutionResult[S] struct
│
├── hooks.go                  # Hook[S] interface + composed hooks
├── otel.go                   # OTelHook[S] implementation
│
├── checkpoint/
│   ├── types.go              # Checkpoint struct + Manager interface
│   ├── memory.go             # InMemoryManager
│   └── file.go               # FileManager
│
├── agent/                    # Agent layer (independent package)
│   ├── state.go              # MessageState, Message, ToolCall
│   ├── nodes.go              # Built-in nodes: LLMNode, ToolNode, VectorRetrieveNode, HumanInputNode
│   ├── tools.go              # Tool interface + ToolRegistry
│   ├── agents.go             # Agent patterns: ReAct, RAG, Supervisor
│   ├── memory.go             # Short-term/long-term memory management
│   └── llm.go                # LLM interface definition (dependency injection)
│
├── examples/
│   ├── 01_sequential/        # 3-node linear pipeline (YAML config + code registration)
│   ├── 02_conditional/       # Conditional branch routing
│   ├── 03_parallel/          # Fan-out/fan-in
│   ├── 04_loop/              # Self-loop + exit condition
│   ├── 05_streaming/         # Streaming nodes
│   ├── 06_checkpoint/        # Persistence and recovery
│   ├── 07_http_server/       # HTTP server example (showing how to use as a server)
│   ├── 08_simple_chat/       # Simple chat Agent
│   ├── 09_rag_agent/         # RAG Agent
│   ├── 10_react_agent/       # ReAct Agent
│   └── 11_supervisor_agent/  # Multi-Agent collaboration
│
└── docs/
    ├── ai-agent-frameworks-survey.md   # AI Agent framework survey
    ├── graphflow-design.md             # This document
    ├── core-guide.md                   # Core layer usage guide
    └── agent-guide.md                  # Agent layer usage guide
```

**Package Organization Notes:**

- **Root package** (`graphflow`): Core layer; users `import "github.com/wzhongyou/graphflow"` to access all core APIs.
- **agent package** (`graphflow/agent`): Agent layer; users `import "github.com/wzhongyou/graphflow/agent"` as needed.
- **checkpoint sub-package**: Independent sub-package with its own persistence concerns, decoupled from the core engine.
- **examples**: Contains both Core layer and Agent layer examples.

---

## 9. API Sketch

### 9.1 Config File Approach (Recommended)

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
// Code side: register node implementations
registry := gf.NewRegistry[TextState]()
registry.RegisterNode("text_transformer", toUpperFunc)
registry.RegisterNode("text_reverser", reverseFunc)
registry.RegisterNode("text_finalizer", finalizeFunc)

// Load from config file
graph, err := gf.LoadFromFile[TextState]("workflow.yaml", registry)
if err != nil {
    panic(err)
}
// LoadFromFile internally calls Compile(); no need to manually compile
```

Full example (conditional + parallel + loop):

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

### 9.2 Programmatic Construction (Dynamic Scenarios)

```go
g := gf.NewGraph[MyState]("dynamic-graph")

g.AddNode("A", func(ctx context.Context, s MyState) (MyState, error) {
    return s, nil
})

g.SetEntryPoint("A")
g.AddEdge("A", "B")

// Conditional branch
g.AddCondition("A", gf.Condition[MyState]{
    If:     func(ctx context.Context, s MyState) bool { return s.Score > 0.5 },
    Target: "B",
})
g.AddCondition("A", gf.Condition[MyState]{
    Target: "C", // If is nil = fallback
})

// Convergence merge
g.SetMergeFunc("D", func(ctx context.Context, parent MyState, branches []MyState) (MyState, error) {
    return merged, nil
})

// Loop
g.SetMaxIterations("A", 100)
g.SetLoopExit("A", func(ctx context.Context, s MyState) bool {
    return s.Count < 10
})

if err := g.Compile(); err != nil {
    panic(err)
}
```

### 9.3 Hybrid Mode

```go
// Load base structure from file
graph, _ := gf.LoadFromFile[MyState]("base.yaml", registry)

// Extend programmatically
graph.AddNode("extra_validation", validateFunc)
graph.AddEdge("summarize", "extra_validation")
graph.Compile() // Re-compile and validate
```

### 9.4 Execution and Recovery

```go
engine := gf.NewEngine(graph)

// Normal execution
result, err := engine.Run(ctx, initialState,
    gf.WithHook(otelHook),
    gf.WithCheckpoint(store),
    gf.WithCheckpointFreq(1),
)

// Recover from checkpoint
result, err := engine.Run(ctx, nil,  // nil state → use state from checkpoint
    gf.WithResumeFrom(checkpointID),
)
```

---

## 10. Implementation Phases

### Core Layer Phases

| Phase | Content | Deliverable |
|-------|---------|-------------|
| **P1** | Graph model + sequential execution + config files | `graph.go`, `config.go`, `registry.go`, `engine.go`; YAML definition + code registration runnable |
| **P2** | Conditional routing | State-value-based branch selection |
| **P3** | Parallel fan-out/fan-in | goroutine concurrency + MergeFunc |
| **P4** | Loops | Back edge detection + iteration control + exit conditions |
| **P5** | Streaming | Stream[T] type and utility functions |
| **P6** | Checkpoint/recovery | Manager interface + FileManager + InMemoryManager |
| **P7** | Observability | Hook interface + OTelHook + DOT output |
| **P8** | Result encapsulation | ExecutionResult[S] struct, unified return format |

### Agent Layer Phases (start after Core P1-P4 completion)

| Phase | Content | Deliverable |
|-------|---------|-------------|
| **A1** | Agent state abstraction | `agent/state.go`, MessageState, Message, ToolCall |
| **A2** | LLM interface definition | `agent/llm.go`, unified LLM invocation interface (supports OpenAI, Anthropic, Gemini) |
| **A3** | Built-in nodes | `agent/nodes.go`, LLMNode, ToolNode, VectorRetrieveNode, HumanInputNode |
| **A4** | Tool system | `agent/tools.go`, Tool interface + ToolRegistry + built-in tools (HTTP, DB, Calculator) |
| **A5** | Memory management | `agent/memory.go`, ShortTermMemory, LongTermMemory |
| **A6** | Agent pattern - ReAct | `agent/agents.go`, ReAct Agent pattern |
| **A7** | Agent pattern - RAG | `agent/agents.go`, RAG Agent pattern |
| **A8** | Agent pattern - Supervisor | `agent/agents.go`, Multi-Agent collaboration pattern |
| **A9** | Streaming Agent | `agent/stream.go`, streaming LLM nodes + SSE responses |
| **A10** | Structured Output | `agent/structured.go`, enforce LLM to output JSON Schema-compliant structs, with auto-retry and parsing |
| **A11** | Prompt Template | `agent/prompt.go`, variable interpolation, few-shot example management, chat/completion formats |
| **A12** | Summary Memory | `agent/memory.go` extension, invoke LLM to compress old messages into a summary when context window overflows |
| **A13** | RAG Document Pipeline | `agent/rag/`, Document Loader (PDF/HTML/Markdown/DB), Chunking strategies (fixed/semantic splitting), Hybrid retrieval (Dense + Sparse + Rerank) |
| **A14** | Code Execution Sandbox | `agent/tools/sandbox/`, Tool-level process isolation (Docker / WASM), prevents LLM-generated code from executing directly on the host |
| **A15** | Guardrails | `agent/guardrails/`, Input/output filter interfaces, PII detection, sensitive content validation, pluggable before/after any node |
| **A16** | Graph-Level Event Stream | `graph/engine.go` extension, `RunWithEvents` returns node-level/token-level event stream, supports external SSE passthrough |
| **A17** | MCP Protocol Support | `agent/mcp/`, MCP Client (connect to external tool servers) + MCP Server (expose local ToolRegistry as an MCP service) |
| **A18** | A2A Protocol Support | `agent/a2a/`, Agent-to-Agent cross-language collaboration, compatible with Google ADK A2A spec, supports heterogeneous Agent orchestration |

### Documentation and Examples

| Phase | Content | Deliverable |
|-------|---------|-------------|
| **D1** | README | Project introduction, quick start, core concepts |
| **D2** | Core Usage Guide | `docs/core-guide.md`, complete Core layer documentation |
| **D3** | Agent Usage Guide | `docs/agent-guide.md`, complete Agent layer documentation |
| **D4** | Core Examples | `examples/01-07`, 7 Core layer examples |
| **D5** | Agent Examples | `examples/08-11`, 4 Agent layer examples |
| **D6** | Best Practices | Deployment guide, performance optimization, troubleshooting |

### Priority Notes

**MVP (Minimum Viable Product)**: Core layer P1-P7 + Agent layer A1-A4 + Documentation D1-D2-D4

**Production-Ready**: Core layer P1-P8 + Agent layer A1-A16 + Documentation D1-D6 + Full examples

**Protocol Interoperability**: Production-ready plus A17 (MCP) + A18 (A2A)

---

## 11. Edge Cases and Decision Records

### Confirmed Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Config file format | **YAML** | Natural nested array expression, cloud-native ecosystem standard, familiar to target users |

### 11.1 Empty Graph

`Compile()` returns an error; empty graph execution is not allowed.

### 11.2 Unreachable Nodes

`Compile()` outputs a warning (not an error), since unreachable nodes may be reserved for future use.

### 11.3 Parallel Branch Failure

Cancel sibling branches via `context.WithCancel`. Return the first error (or wrap as multierror).

### 11.4 Infinite Loop Prevention

Two-layer protection: (1) `maxIterations` limit (default 1000); (2) user-provided `loopExit` condition. Error when neither is satisfied.

### 11.5 Nil State Return

If a node returns `nil` state (zero value + nil error), the engine treats it as a no-op and continues with the input state.

### 11.6 Node Timeout

Each node is wrapped with `context.WithTimeout` for independent timeout control; timeout returns `context.DeadlineExceeded`.

### 11.7 Concurrent Checkpoint Writes

Manager implementations must be thread-safe. The engine may concurrently call `Save` after parallel branches complete.

---

## 12. Relationship with the AI Agent Layer

### 12.1 Layered Architecture

The current design keeps the Core layer (graphflow) AI-agnostic. The AI Agent layer is built as an independent package (graphflow/agent) on top:

```
┌─────────────────────────────────────────────┐
│  Application Layer (user code)               │
│  - Business workflows: order processing,    │
│    data cleaning                             │
│  - Agent applications: RAG, multi-Agent      │
│    collaboration                             │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│  Agent Layer (graphflow/agent package)       │
│  - MessageState state abstraction            │
│  - Built-in nodes: LLMNode, ToolNode         │
│  - Agent patterns: ReAct, RAG, Supervisor    │
│  - Tool system: ToolRegistry                 │
│  - Memory management: Short/LongTermMemory   │
└─────────────────────────────────────────────┘
                    ↓ depends on
┌─────────────────────────────────────────────┐
│  Core Layer (graphflow package)              │
│  - Graph[S], Engine                          │
│  - Parallel / loop / conditional routing     │
│  - Checkpoint, Hook, OTel                    │
│  - Stream[T], ExecutionResult                │
└─────────────────────────────────────────────┘
```

### 12.2 Agent Layer Core Abstractions

#### State Abstraction

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

#### Built-in Nodes

```go
// LLM invocation node
type LLMNode struct {
    Model     LLMModel
    Prompt    string
    Stream    bool
}

// Tool invocation node
type ToolNode struct {
    Tools []Tool
}

// Vector retrieval node
type VectorRetrieveNode struct {
    Embedder    Embedder
    VectorStore VectorStore
    TopK        int
}

// Human input node (HITL)
type HumanInputNode struct {
    Prompt  string
    Timeout time.Duration
}
```

#### Tool System

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

#### Agent Patterns

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

### 12.3 Usage Examples

#### Simple Chat Agent

```go
import (
    "github.com/wzhongyou/graphflow"
    "github.com/wzhongyou/graphflow/agent"
)

func main() {
    // Configure LLM
    llm := openai.NewChatModel("gpt-4")

    // Create registry
    registry := gf.NewRegistry[*agent.MessageState]()

    // Register LLM node
    registry.RegisterNode("chat", agent.NewLLMNode(agent.LLMNodeConfig{
        Model:  llm,
        Prompt: "You are a helpful assistant.",
    }))

    // Build graph
    graph := gf.NewGraph[*agent.MessageState]("simple-chat")
    graph.SetEntryPoint("chat")

    if err := graph.Compile(); err != nil {
        panic(err)
    }

    // Execute
    engine := gf.NewEngine(graph)

    result, err := engine.Run(ctx, &agent.MessageState{
        Messages: []agent.Message{
            {Role: agent.RoleUser, Content: "Hello!"},
        },
    })

    fmt.Println(result.Messages[len(result.Messages)-1].Content)
}
```

#### RAG Agent

```go
func main() {
    // Use built-in RAG Agent
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
            {Role: agent.RoleUser, Content: "How do I use Graphflow?"},
        },
    })

    fmt.Println(result.Messages[len(result.Messages)-1].Content)
}
```

### 12.4 Deployment Modes

#### Embedded as a Library (Recommended)

```go
// main.go
package main

import (
    "net/http"
    "github.com/wzhongyou/graphflow"
    "github.com/wzhongyou/graphflow/agent"
)

func main() {
    // Load Agent graph
    ragAgent := agent.NewRAGAgent(...)
    graph, _ := ragAgent.BuildGraph()
    engine := gf.NewEngine(graph)

    // Expose HTTP endpoint
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

Deployment:

```bash
# Build
go build -o rag-service

# Run
./rag-service
```

#### Server Mode via Example Code

In `examples/07_http_server`, demonstrating how to:
- Expose as an HTTP service
- Support streaming responses (SSE)
- Integrate Checkpoint recovery
- Add authentication and rate limiting

**Note**: Graphflow does not provide a formal Server implementation, only example code showing how to use it as a server.

---

## 13. Result Encapsulation and Termination

### 13.1 ExecutionResult Structure

```go
type ExecutionResult[S any] struct {
    // Final state
    FinalState S `json:"final_state"`

    // Execution metadata
    GraphName   string    `json:"graph_name"`
    ExecutionID string    `json:"execution_id"`
    StartTime   time.Time `json:"start_time"`
    EndTime     time.Time `json:"end_time"`

    // Termination reason
    Termination TerminationReason `json:"termination"`
    Error       error             `json:"error,omitempty"`

    // Metrics
    NodeCount     int           `json:"node_count"`
    TotalNodes    int           `json:"total_nodes"`
    TotalSteps    int           `json:"total_steps"`
    TotalDuration time.Duration `json:"total_duration"`

    // Checkpoint
    CheckpointID string `json:"checkpoint_id,omitempty"`

    // Trace info (if OTel enabled)
    TraceID string `json:"trace_id,omitempty"`
    SpanID  string `json:"span_id,omitempty"`
}

type TerminationReason string

const (
    TerminationCompleted TerminationReason = "completed"  // Normal completion
    TerminationCancelled TerminationReason = "cancelled"  // Context cancelled
    TerminationError     TerminationReason = "error"      // Node error
    TerminationTimeout   TerminationReason = "timeout"    // Timeout
    TerminationMaxSteps  TerminationReason = "max_steps"  // Max steps reached
)
```

### 13.2 Engine API Update

```go
// Run returns ExecutionResult
func (e *Engine[S]) Run(ctx context.Context, initialState S, opts ...Option) (*ExecutionResult[S], error)

// RunStream returns streaming result
func (e *Engine[S]) RunStream(ctx context.Context, initialState S, opts ...Option) (*StreamResult[S], error)

// RunWithEvents returns event stream
func (e *Engine[S]) RunWithEvents(ctx context.Context, initialState S, opts ...Option) (*EventResult[S], error)
```

### 13.3 Terminal Node Support

```go
// Mark a terminal node
func (g *Graph[S]) MarkTerminalNode(name string, reason TerminationReason)

// Example
g.AddNode("success", successNode)
g.MarkTerminalNode("success", TerminationCompleted)

g.AddNode("failure", failureNode)
g.MarkTerminalNode("failure", TerminationError)
```

---

## 14. Deployment Models

### 14.1 Embedded as a Library (Recommended)

Graphflow is a library; it does not provide a standalone server.

**Advantages**:
- Best performance: no network overhead, no serialization
- High flexibility: deep integration into existing systems
- Simple deployment: single binary
- Dependency management: no external service dependencies

**Use Cases**:
- Go microservice orchestration
- Internal toolchains
- High-performance scenarios
- Workflows within monolithic applications

**Usage**:

```go
// main.go
package main

import (
    "net/http"
    "github.com/wzhongyou/graphflow"
)

func main() {
    // Load graph
    registry := gf.NewRegistry[OrderState]()
    // ... register nodes
    graph, _ := gf.LoadFromFile[OrderState]("order.yaml", registry)
    engine := gf.NewEngine(graph)

    // Expose HTTP endpoint
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

**Deployment**:

```bash
# Build
go build -o order-service

# Run
./order-service
```

### 14.2 As a Standalone Gateway (Example Code)

In `examples/07_http_server`, demonstrating how to build an independent graph execution service:

```go
// examples/07_http_server/main.go
package main

import (
    "github.com/wzhongyou/graphflow"
)

func main() {
    server := NewGraphflowServer()

    // Dynamically register graphs (from database/config center)
    server.RegisterGraph("order-process", loadOrderGraph())
    server.RegisterGraph("rag-agent", loadRAGAgent())

    server.Listen(":8080")
}
```

**Note**: This is only example code showing how to use Graphflow to build a server. Graphflow itself does not provide a formal Server implementation.

### 14.3 Deployment Options

| Option | Description | Use Case |
|--------|-------------|----------|
| **Embedded in microservice** | Graphflow embedded as a library in existing services | Go microservices, internal systems |
| **Standalone Gateway** | Build an independent service using example code | Multi-language architectures, unified management |
| **Serverless** | Deploy to Cloud Run / Lambda | Cloud-native, on-demand scaling |
| **K8s Deployment** | Deploy via Kubernetes | Enterprise-grade, high availability |

### 14.4 Comparison with LangChain/LangGraph

| Dimension | Graphflow | LangChain/LangGraph |
|-----------|-----------|---------------------|
| **Deployment mode** | Library mode | Library mode + Langserve (3rd party) |
| **Performance** | Best (no network overhead) | Depends on network serialization |
| **Flexibility** | Very high | Medium |
| **Multi-language support** | Requires standalone server | Via Langserve |
| **Learning curve** | Go developer friendly | Python developer friendly |

---

## 15. Design Decisions Under Discussion

### Core Layer Decisions

1. **Compile-time vs. Runtime Validation**: `Compile()` does static validation (node existence, edge references, loop detection) but retains some flexibility (e.g., allowing unreachable nodes). Are there other items that should be checked in `Compile()`?

2. **Default Parallel Strategy**: When a node has multiple normal outgoing edges, should they execute in parallel or sequentially by default? The current design defaults to parallel, as this is the more common high-performance scenario.

3. **State Granularity**: Currently a graph has a single state type `S`. If sub-graphs need independent state types later, should this be supported? (Similar to Eino's Workflow pattern with struct field-level data mapping.)

4. **Error Recovery Granularity**: Currently retry is node-level. Should graph-level global error handling be supported (e.g., re-running from the start on failure)?

5. **Streaming + Checkpoint**: What are the checkpoint semantics during streaming? Should we wait for the stream to be fully consumed before checkpointing? Recommended to flag this as a limitation during P5/P6 phases.

6. **Conditional Expressions in Config**: Currently conditional edges reference code functions by name. Should simple expressions be supported in config (e.g., `score > 0.5`) to reduce code registration overhead?

### Agent Layer Decisions

7. **LLM Interface Design**: ✅ Decided. Supports streaming calls (`ChatStream`) + Function Calling (`ChatRequest.Tools`). LLM access is injected through the llmgate adapter layer; the LLMModel interface stays minimal, with provider details isolated in the adapter layer.

8. **MCP Protocol Support**: Planned as phase A17. MCP Client connects to external tool servers; MCP Server exposes tools registered in ToolRegistry as standard MCP services, facilitating interoperability with other AI frameworks.

9. **A2A Protocol Support**: Planned as phase A18, compatible with Google ADK A2A specification, allowing Graphflow Agents to be invoked by Agent frameworks in other languages.

10. **Vector Store Interface**: How generic should the vector store interface be? Should it support pgvector, Milvus, Weaviate, etc.?

11. **Human-in-the-Loop Patterns**: How many HITL patterns should be supported? (Reference: Eino supports 5-8 patterns.)

12. **Memory Persistence**: How should short-term/long-term memory be persisted? Is Redis/S3 support needed?

### Architecture Decisions

13. **Server Mode**: Should a formal Server implementation be provided, or just example code? Current decision: example code only.

14. **Modularization Level**: Should the Agent layer be further split? (e.g., `agent/llm`, `agent/tools`, `agent/memory` as independent packages.)

15. **Testing Strategy**: How should the Agent layer be tested? Is a Mock LLM needed?

### Product Decisions

16. **MVP Scope**: What features should the MVP include? Core layer P1-P7? Agent layer A1-A4?

17. **Documentation Priority**: Which docs must be completed first? README, Core Guide, Agent Guide?

18. **Example Priority**: Which examples must be completed first? Sequential, conditional, parallel, loop, checkpoint, simple Agent?

### Performance Decisions

19. **Parallelism Control**: Should parallelism be capped? (To avoid goroutine explosion.)

20. **Memory Optimization**: Can state deep copy be optimized? (Using pointers, shared memory.)

---

## 16. Reference Implementation Comparisons

### Comparison with LangGraph

| Feature | Graphflow | LangGraph |
|---------|-----------|-----------|
| **Language** | Go | Python |
| **State model** | Generic S | StateGraph + Reducer |
| **Configuration** | YAML + Code | Pure code |
| **Checkpoint** | Interface + multiple implementations | Built-in multiple storage backends |
| **Observability** | Hook + OTel | LangSmith |
| **Deployment** | Embedded | Embedded + Langserve |
| **Learning curve** | Moderate (Go prerequisite) | High (LangChain prerequisite) |
| **Performance** | Best | Moderate (Python) |

### Comparison with Eino

| Feature | Graphflow | Eino |
|---------|-----------|------|
| **Language** | Go | Go |
| **Orchestration API** | Graph | Graph + Chain + Workflow |
| **Configuration** | YAML + Code | Pure code |
| **HITL** | Planned | Supports 5-8 patterns |
| **Streaming** | Explicit streams | Framework-level automatic streaming |
| **Deployment** | Embedded | Embedded + CloudWeGo |
| **Production validation** | New project | Hundreds of services at ByteDance |
| **AI capabilities** | Agent layer planned | Mature ADK module |

### Comparison with Google ADK Go

| Feature | Graphflow | Google ADK Go |
|---------|-----------|---------------|
| **Language** | Go | Go |
| **Configuration** | YAML + Code | YAML |
| **A2A support** | Planned | Supported (Linux Foundation) |
| **Cloud-native** | General purpose | Deep Cloud Run integration |
| **OTel** | Built-in | Native integration |
| **Agent patterns** | Planned | Self-healing, plugins |

---

## 17. Key Milestones

### MVP Release (est. 2026-06)

**Core Layer**:
- ✅ Graph model + sequential execution
- ✅ Conditional routing
- ✅ Parallel fan-out/fan-in
- ✅ Loop control
- ✅ Checkpoint
- ✅ Hook + OTel

**Agent Layer**:
- ✅ MessageState abstraction
- ✅ LLM interface
- ✅ Basic nodes (LLM, Tool)
- ✅ Tool system

**Documentation and Examples**:
- ✅ README
- ✅ Core Guide
- ✅ 6 Core examples
- ✅ 2 Agent examples

### v0.2 Release (est. 2026-07)

**Agent Layer Enhancements**:
- ✅ RAG Agent pattern
- ✅ ReAct Agent pattern
- ✅ Memory management
- ✅ Streaming LLM

**Documentation and Examples**:
- ✅ Agent Guide
- ✅ 4 Agent examples

### v0.3 Release (est. 2026-08)

**Advanced Features**:
- ✅ Supervisor Agent pattern
- ✅ Human-in-the-Loop
- ✅ Vector retrieval nodes

**Enterprise Features**:
- ✅ Redis Checkpoint
- ✅ Circuit breaker
- ✅ Rate limiting

### v0.4 Release (est. 2026-11)

**Agent Capability Completion**:
- ✅ Structured Output, enforce LLM JSON Schema output (A10)
- ✅ Prompt Template, variable interpolation + few-shot management (A11)
- ✅ Summary Memory, LLM compression of overflow messages (A12)
- ✅ RAG Document Pipeline, Loader / Chunker / Hybrid retrieval (A13)
- ✅ Code Execution Sandbox, Docker / WASM process isolation (A14)
- ✅ Guardrails, input/output filtering + PII detection (A15)
- ✅ Graph-Level Event Stream, RunWithEvents / SSE passthrough (A16)

### v1.0 Release (est. 2026-12)

**Production-Ready**:
- ✅ Complete documentation
- ✅ Performance optimization
- ✅ Stability assurance
- ✅ Community ecosystem

**Protocol Interoperability**:
- ✅ MCP Client / Server (A17)
- ✅ A2A cross-language Agent collaboration (A18)

---

## 18. Development Guide

### Contribution Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Create a Pull Request

### Code Standards

- Follow `gofmt` formatting
- Follow `golint` checks
- Unit tests required
- Complex logic must have comments
- Public API must have documentation

### Testing Requirements

- Unit test coverage > 80%
- Integration tests covering major scenarios
- Performance tests against baseline
- Concurrency tests (race detector)

---

## 19. License

This project is licensed under the MIT License.

---

## 20. Contact

- Author: wangzhongyou
- Repository: https://github.com/wzhongyou/graphflow
- Documentation: https://github.com/wzhongyou/graphflow/docs
- Discussions: https://github.com/wzhongyou/graphflow/discussions
