package graph

// OTelHook is a Hook implementation that emits OpenTelemetry spans and metrics.
//
// TODO(P7): implement using go.opentelemetry.io/otel
//   spans: graph.graph.<name>, graph.node.<name>, graph.checkpoint.*
//   metrics: graph.node.duration_ms, graph.node.executions, graph.graph.duration_ms
type OTelHook[S any] struct{}
