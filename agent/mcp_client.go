package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPClientAdapter connects to an MCP server and provides its tools as agent.Tool
// instances. It uses the stdio transport to spawn and communicate with an MCP
// server process.
type MCPClientAdapter struct {
	mcpClient *client.Client
	tools     []Tool
}

// NewMCPClientAdapter creates a new MCP client adapter that spawns an MCP server
// process. command is the executable path and args are optional command-line
// arguments for the server.
func NewMCPClientAdapter(command string, args ...string) (*MCPClientAdapter, error) {
	c, err := client.NewStdioMCPClient(command, nil, args...)
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to create client: %w", err)
	}
	return &MCPClientAdapter{mcpClient: c}, nil
}

// Connect initializes the MCP connection and discovers available tools. It must
// be called before Tools() returns any results.
func (a *MCPClientAdapter) Connect(ctx context.Context) error {
	initReq := mcp.InitializeRequest{}
	initReq.Params = mcp.InitializeParams{
		ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		ClientInfo: mcp.Implementation{
			Name:    "graphflow",
			Version: "1.0.0",
		},
	}

	_, err := a.mcpClient.Initialize(ctx, initReq)
	if err != nil {
		return fmt.Errorf("mcp: initialize failed: %w", err)
	}

	// Discover tools from the server.
	toolsResult, err := a.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("mcp: list tools failed: %w", err)
	}

	for _, t := range toolsResult.Tools {
		a.tools = append(a.tools, &mcpToolAdapter{
			client:      a.mcpClient,
			name:        t.Name,
			description: t.Description,
			inputSchema: t.InputSchema,
		})
	}

	return nil
}

// Tools returns the discovered agent.Tool instances. Returns nil if Connect has
// not been called yet.
func (a *MCPClientAdapter) Tools() []Tool {
	return a.tools
}

// Close terminates the MCP connection and cleans up the server process.
func (a *MCPClientAdapter) Close() error {
	return a.mcpClient.Close()
}

// mcpToolAdapter wraps an MCP tool as an agent.Tool.
type mcpToolAdapter struct {
	client      *client.Client
	name        string
	description string
	inputSchema mcp.ToolInputSchema
}

func (t *mcpToolAdapter) Name() string          { return t.name }
func (t *mcpToolAdapter) Description() string    { return t.description }
func (t *mcpToolAdapter) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": t.inputSchema.Properties,
		"required":   t.inputSchema.Required,
	}
}

func (t *mcpToolAdapter) Execute(ctx context.Context, args map[string]any) (string, error) {
	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = t.name
	callReq.Params.Arguments = args

	result, err := t.client.CallTool(ctx, callReq)
	if err != nil {
		return "", fmt.Errorf("mcp: call tool %q: %w", t.name, err)
	}

	if result.IsError {
		return "", fmt.Errorf("mcp: tool %q returned error", t.name)
	}

	// Extract text content from the response.
	var sb strings.Builder
	for _, content := range result.Content {
		if textContent, ok := mcp.AsTextContent(content); ok {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(textContent.Text)
		}
	}

	return sb.String(), nil
}
