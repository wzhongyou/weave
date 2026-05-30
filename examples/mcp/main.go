// mcp_demo 展示 MCP 客户端连接——通过 MCP 协议发现和调用工具。
//
//  用法:
//    go run ./examples/mcp -cmd "npx" -args "-y,@modelcontextprotocol/server-everything"
//
//  这连接到 MCP 的 "everything" 参考服务器，列出工具并调用 echo 工具。
//  如果不想安装 Node.js，也可以连接任何 stdio 协议的 MCP 服务器。
//
//  或者使用内置的简易计算器 MCP 服务器（见示例末尾）。
//
// 需求:
//   - 需要 go 1.21+
//   - 如果使用外部 MCP 服务器，需要对应的运行时（Node.js, Python 等）
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wzhongyou/graphflow/agent"
)

func main() {
	cmd := flag.String("cmd", "", "MCP 服务器命令 (如 npx, python)")
	args := flag.String("args", "", "MCP 服务器参数 (逗号分隔)")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 处理 Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if *cmd != "" {
		runWithExternalMCP(ctx, *cmd, parseArgs(*args))
	} else {
		runWithDemoMCP(ctx)
	}
}

func parseArgs(s string) []string {
	if s == "" {
		return nil
	}
	// 简单按逗号分割
	return []string{s}
}

func runWithExternalMCP(ctx context.Context, command string, args []string) {
	fmt.Printf("🔗 连接到 MCP 服务器: %s %v\n", command, args)

	adapter, err := agent.NewMCPClientAdapter(command, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建 MCP 客户端失败: %v\n", err)
		os.Exit(1)
	}
	defer adapter.Close()

	if err := adapter.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "连接 MCP 服务器失败: %v\n", err)
		os.Exit(1)
	}

	tools := adapter.Tools()
	fmt.Printf("发现 %d 个工具:\n", len(tools))
	for _, tool := range tools {
		fmt.Printf("  - %s: %s\n", tool.Name(), tool.Description())
	}

	// 如果有工具，调用第一个
	if len(tools) > 0 {
		tool := tools[0]
		fmt.Printf("\n调用工具: %s\n", tool.Name())
		result, err := tool.Execute(ctx, map[string]any{})
		if err != nil {
			fmt.Printf("调用失败: %v\n", err)
		} else {
			fmt.Printf("结果: %s\n", result)
		}
	}
}

func runWithDemoMCP(ctx context.Context) {
	fmt.Println("=== MCP 客户端演示 ===")
	fmt.Println()
	fmt.Println("用法: go run ./examples/mcp -cmd \"<mcp-server-command>\" -args \"<args>\"")
	fmt.Println()
	fmt.Println("示例: go run ./examples/mcp -cmd \"npx\" -args \"-y,@modelcontextprotocol/server-everything\"")
	fmt.Println()
	fmt.Println("或者用任意 stdio MCP 服务器:")
	fmt.Println("  go run ./examples/mcp -cmd \"python\" -args \"-m,mcp_server\"")
	fmt.Println()
	fmt.Println("没装 MCP 服务器？试试代理模式：")
	fmt.Println("  先装 @modelcontextprotocol/server-everything:")
	fmt.Println("    npx @modelcontextprotocol/server-everything")
	fmt.Println("  然后在另一个终端运行:")
	fmt.Println("    go run ./examples/mcp -cmd npx -args \"-y,@modelcontextprotocol/server-everything\"")

	// 展示 MCP 客户端适配器的 API 用法
	fmt.Println("\n── MCPClientAdapter API ──")
	fmt.Println("1. adapter, err := agent.NewMCPClientAdapter(command, args...)")
	fmt.Println("2. err := adapter.Connect(ctx)")
	fmt.Println("3. tools := adapter.Tools()  // 返回 []agent.Tool")
	fmt.Println("4. result, err := tool.Execute(ctx, args)")
	fmt.Println("5. adapter.Close()")
	fmt.Println()
	fmt.Println("返回的 Tool 可以直接传给 ReActAgent：")
	fmt.Println("  agent.NewReActAgent(agent.ReActAgentConfig{")
	fmt.Println("    Tools: adapter.Tools(),")
	fmt.Println("  })")
}
