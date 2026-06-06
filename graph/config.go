package graph

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config is the top-level structure for a YAML workflow definition.
type Config struct {
	Name    string    `yaml:"name"`
	Version string    `yaml:"version"`
	Entry   string    `yaml:"entry"`
	Include []string  `yaml:"include,omitempty"`
	Nodes   []NodeDef `yaml:"nodes"`
	Edges   []EdgeDef `yaml:"edges"`
	Loops   []LoopDef `yaml:"loops,omitempty"`
}

// NodeDef describes a single node in the config.
type NodeDef struct {
	Name    string    `yaml:"name"`
	Type    string    `yaml:"type"`
	Timeout string    `yaml:"timeout,omitempty"`
	Retry   *RetryDef `yaml:"retry,omitempty"`
}

// RetryDef holds retry policy parameters.
type RetryDef struct {
	MaxAttempts int    `yaml:"max_attempts"`
	Backoff     string `yaml:"backoff,omitempty"`
	MaxBackoff  string `yaml:"max_backoff,omitempty"`
}

// EdgeDef describes an edge (or fan-out/fan-in) in the config.
type EdgeDef struct {
	From      OneOrMany `yaml:"from"`
	To        OneOrMany `yaml:"to,omitempty"`
	Condition string    `yaml:"condition,omitempty"`
	Branches  []Branch  `yaml:"branches,omitempty"`
	Merge     string    `yaml:"merge,omitempty"`
}

// Branch is one arm of a conditional edge.
type Branch struct {
	When string `yaml:"when"` // "default" = fallback
	To   string `yaml:"to"`
}

// LoopDef configures loop behaviour for a node.
type LoopDef struct {
	Node          string `yaml:"node"`
	MaxIterations int    `yaml:"max_iterations,omitempty"`
	ExitCondition string `yaml:"exit_condition,omitempty"`
}

// OneOrMany deserialises a YAML field that can be a single string or a list.
type OneOrMany []string

// UnmarshalYAML implements yaml.Unmarshaler so a OneOrMany field accepts both
//
//	from: "single_node"
//
// and
//
//	from: ["node_a", "node_b"]
func (o *OneOrMany) UnmarshalYAML(value *yaml.Node) error {
	var single string
	if err := value.Decode(&single); err == nil {
		*o = OneOrMany{single}
		return nil
	}
	var many []string
	if err := value.Decode(&many); err != nil {
		return fmt.Errorf("oneOrMany: expected a string or a list of strings, got %s", value.Tag)
	}
	*o = OneOrMany(many)
	return nil
}

// Slice returns the underlying []string.
func (o OneOrMany) Slice() []string { return []string(o) }
