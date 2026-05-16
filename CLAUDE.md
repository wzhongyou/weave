# CLAUDE.md

## Project Overview

Graphflow is a Go-native Agent development framework. The graph execution engine (`graph/`) is the AI-agnostic core; the Agent layer (`agent/`) builds on top of it.

## Essential Commands

```bash
go build ./...          # build all packages
go vet ./...            # static analysis (must pass before any commit)
go test ./...           # run all tests
go test ./graph/...     # run graph engine tests only
```

## Package Structure

```
graph/          # Core engine — Graph[S], Engine[S], NodeFunc[S]
  middleware/   # NodeFunc decorators (retry, timeout, circuit breaker, …)
  node/         # Built-in nodes (HTTP, Delay, Transform, Noop)
  checkpoint/   # Persistence backends (memory, file, redis, sqlite)
agent/          # Agent abstractions — MessageState, LLMNode, ToolNode, …
examples/
  agent_demo/   # ReAct agent with Hook tracing
docs/           # Design docs and guides
```

## Key Conventions

- `graph/` has **zero external dependencies** — do not add any imports outside stdlib.
- `agent/` may import `graph/` but never vice versa.
- External integrations (Redis, SQLite, OTel) go in sub-packages so the core stays dependency-free.
- All public types use Go generics (`Graph[S]`, `Engine[S]`, `Hook[S]`) — keep the state type `S` as the single generic parameter per graph.
- Node functions match exactly: `func(ctx context.Context, state S) (S, error)`.
- Middleware wraps `NodeFunc[S]` and returns `NodeFunc[S]` — composable by design.

## Implementation Status

Use `TODO(Px)` / `TODO(Ax)` markers that match the roadmap phases in the design doc (`docs/graphflow-design.md`):

| Marker | Meaning |
|--------|---------|
| `TODO(P1)` | Core P1 — graph model, sequential engine, YAML config |
| `TODO(P3)` | Parallel fan-out / fan-in |
| `TODO(P4)` | Loop / back-edge handling |
| `TODO(P5)` | Stream[T] |
| `TODO(P6)` | Checkpoint persistence |
| `TODO(P7)` | OTel hook |
| `TODO(A3)`–`TODO(A8)` | Agent layer phases |

## Design Decisions

See `docs/graphflow-design.md` for full rationale. Key ones:

- **Pregel-style execution**: superstep loop, not recursive calls.
- **Back edges = loops**: detected by DFS at `Compile()` time; `SetMaxIterations` guards against infinite loops (default 1000).
- **Conditional edges take priority** over unconditional edges; first match wins.
- **Multiple unconditional edges = fan-out** (parallel, implemented in `engine_parallel.go`).
- **Hook is stored as `any`** in `runConfig` and type-asserted in `hookOf[S]` — avoids making `Option` generic at the cost of a silent no-op on type mismatch.
