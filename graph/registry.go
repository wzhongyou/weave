package graph

import "context"

// Registry holds all named components referenced by config files.
type Registry[S any] struct {
	nodes      map[string]NodeFunc[S]
	conditions map[string]func(ctx context.Context, state S) string
	merges     map[string]MergeFunc[S]
	exits      map[string]func(ctx context.Context, state S) bool
}

// NewRegistry creates an empty registry.
func NewRegistry[S any]() *Registry[S] {
	return &Registry[S]{
		nodes:      make(map[string]NodeFunc[S]),
		conditions: make(map[string]func(ctx context.Context, state S) string),
		merges:     make(map[string]MergeFunc[S]),
		exits:      make(map[string]func(ctx context.Context, state S) bool),
	}
}

// RegisterNode registers a node implementation under typeName.
func (r *Registry[S]) RegisterNode(typeName string, fn NodeFunc[S]) {
	r.nodes[typeName] = fn
}

// RegisterCondition registers a condition function; it returns the branch name to follow.
func (r *Registry[S]) RegisterCondition(name string, fn func(ctx context.Context, state S) string) {
	r.conditions[name] = fn
}

// RegisterMerge registers a merge function for fan-in nodes.
func (r *Registry[S]) RegisterMerge(name string, fn MergeFunc[S]) {
	r.merges[name] = fn
}

// RegisterExitCondition registers a loop exit predicate (false = exit loop).
func (r *Registry[S]) RegisterExitCondition(name string, fn func(ctx context.Context, state S) bool) {
	r.exits[name] = fn
}
