package graph

import (
	"errors"
	"fmt"
	"strings"
)

// --- 哨兵错误 ---

var (
	ErrGraphNotCompiled  = errors.New("graph not compiled")
	ErrGraphEmpty        = errors.New("graph has no nodes")
	ErrNodeNotFound      = errors.New("node not found")
	ErrEntryNotSet       = errors.New("entry point not set")
	ErrMaxIterations     = errors.New("max iterations reached")
	ErrCircuitOpen       = errors.New("circuit breaker is open")
	ErrBulkheadFull      = errors.New("bulkhead concurrency limit reached")
	ErrValidation        = errors.New("validation failed")
)

// --- 可重试标记 ---

// Retryable wraps an error to signal that the engine should retry the node.
type Retryable struct{ Cause error }

func (e *Retryable) Error() string { return "retryable: " + e.Cause.Error() }
func (e *Retryable) Unwrap() error { return e.Cause }

// IsRetryable reports whether err (or any in its chain) is retryable.
func IsRetryable(err error) bool {
	var r *Retryable
	return errors.As(err, &r)
}

// --- 节点错误 ---

// NodeError wraps an error produced by a specific node execution.
type NodeError struct {
	NodeName string
	Attempt  int
	Cause    error
}

func (e *NodeError) Error() string {
	return fmt.Sprintf("node %q attempt %d: %v", e.NodeName, e.Attempt, e.Cause)
}
func (e *NodeError) Unwrap() error { return e.Cause }

// --- 图执行错误 ---

// GraphError is the top-level error returned by Engine.Run.
type GraphError struct {
	GraphName   string
	ExecutionID string
	Cause       error
}

func (e *GraphError) Error() string {
	return fmt.Sprintf("graph %q execution %q: %v", e.GraphName, e.ExecutionID, e.Cause)
}
func (e *GraphError) Unwrap() error { return e.Cause }

// --- 并行分支多错误 ---

// MultiError aggregates errors from parallel branches.
type MultiError struct{ Errs []error }

func (e *MultiError) Error() string {
	msgs := make([]string, len(e.Errs))
	for i, err := range e.Errs {
		msgs[i] = err.Error()
	}
	return "multiple errors: [" + strings.Join(msgs, "; ") + "]"
}

// Append adds a non-nil error to the collection.
func (e *MultiError) Append(err error) {
	if err != nil {
		e.Errs = append(e.Errs, err)
	}
}

// ToError returns nil if no errors were collected.
func (e *MultiError) ToError() error {
	if len(e.Errs) == 0 {
		return nil
	}
	return e
}

// --- Panic 捕获 ---

// PanicError wraps a recovered panic value.
type PanicError struct {
	NodeName string
	Value    any
	Stack    []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("panic in node %q: %v", e.NodeName, e.Value)
}
