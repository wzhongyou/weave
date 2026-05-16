package agent

import "context"

// Tool is the interface for any capability a ToolNode can invoke.
type Tool interface {
	Name() string
	Description() string
	// Parameters returns a JSON Schema describing the tool's argument object.
	Parameters() map[string]any
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// ToolRegistry holds named tools available to agent nodes.
type ToolRegistry struct {
	tools map[string]Tool
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry { return &ToolRegistry{tools: make(map[string]Tool)} }

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(tool Tool) { r.tools[tool.Name()] = tool }

// Get retrieves a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools.
func (r *ToolRegistry) List() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// TODO(A4): built-in tools — HTTPTool, CalculatorTool
