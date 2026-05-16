# Contributing to Graphflow

Thank you for your interest in contributing!

## Getting Started

```bash
git clone https://github.com/wzhongyou/graphflow.git
cd graphflow
go build ./...
go vet ./...
```

## Reporting Issues

- Search existing issues before opening a new one.
- Include Go version, OS, and a minimal reproducible example.

## Submitting a Pull Request

1. Fork the repo and create a branch from `main`.
2. Keep changes focused — one feature or fix per PR.
3. Ensure `go vet ./...` passes with no warnings.
4. Add or update tests for non-trivial logic.
5. Public APIs must have doc comments.

## Commit Messages

Use the conventional format:

```
<type>: <short summary>

feat: add RAGAgent.BuildGraph
fix: handle nil state in Engine.Run
docs: update ReAct hook example
refactor: simplify back-edge detection in Compile
test: add parallel fan-out integration test
```

Types: `feat` · `fix` · `docs` · `refactor` · `test` · `chore`

## Code Style

- Follow standard Go conventions (`gofmt`, `golint`).
- No comments that describe *what* the code does — only *why* when non-obvious.
- Prefer explicit over clever.

## License

By contributing, you agree your contributions will be licensed under the [MIT License](LICENSE).
