package graph

import "time"

// TerminationReason describes why a graph execution ended.
type TerminationReason string

const (
	TerminationCompleted TerminationReason = "completed"
	TerminationCancelled TerminationReason = "cancelled"
	TerminationError     TerminationReason = "error"
	TerminationTimeout   TerminationReason = "timeout"
	TerminationMaxSteps  TerminationReason = "max_steps"
)

// ExecutionResult is returned by Engine.Run.
type ExecutionResult[S any] struct {
	FinalState    S                 `json:"final_state"`
	GraphName     string            `json:"graph_name"`
	ExecutionID   string            `json:"execution_id"`
	StartTime     time.Time         `json:"start_time"`
	EndTime       time.Time         `json:"end_time"`
	Termination   TerminationReason `json:"termination"`
	Error         error             `json:"error,omitempty"`
	NodeCount     int               `json:"node_count"`
	TotalNodes    int               `json:"total_nodes"`
	TotalSteps    int               `json:"total_steps"`
	TotalDuration time.Duration     `json:"total_duration"`
	CheckpointID  string            `json:"checkpoint_id,omitempty"`
	TraceID       string            `json:"trace_id,omitempty"`
	SpanID        string            `json:"span_id,omitempty"`
}
