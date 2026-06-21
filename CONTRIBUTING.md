# 为 Weave 做贡献

感谢你愿意来帮忙！

## 开始

```bash
git clone https://github.com/wzhongyou/weave.git
cd weave
go build ./...
go vet ./...
```

## 报告问题

- 提交新 issue 前请先搜索已有 issue。
- 请包含 Go 版本、操作系统和最小可复现示例。

## 提交 Pull Request

1. Fork 本仓库并从 `main` 创建分支。
2. 保持改动聚焦——一个 PR 只做一个功能或修复。
3. 确保 `go vet ./...` 零警告通过。
4. 非简单逻辑请添加或更新测试。
5. 公开 API 必须有文档注释。

## 提交信息

使用约定式提交格式：

```
<类型>: <简要说明>

feat: add RAGAgent.BuildGraph
fix: handle nil state in Engine.Run
docs: update ReAct hook example
refactor: simplify back-edge detection in Compile
test: add parallel fan-out integration test
```

类型：`feat` · `fix` · `docs` · `refactor` · `test` · `chore`

## 代码风格

- 遵循标准 Go 规范（`gofmt`、`golint`）。
- 不要写描述代码*做了什么*的注释——只在需要的地方写*为什么*。
- 优先显式而非花哨。

## 许可证

提交即表示你同意你的贡献将适用 [MIT 许可证](LICENSE)。
