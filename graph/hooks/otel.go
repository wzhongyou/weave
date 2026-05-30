// Package hooks provides implementations of graph.Hook.
package hooks

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OTelHook implements graph.Hook with OpenTelemetry tracing.
type OTelHook[S any] struct {
	tracer     trace.Tracer
	graphSpans map[string]trace.Span
	nodeSpans  map[string]trace.Span
}

// NewOTelHook creates an OTelHook with a named tracer.
func NewOTelHook[S any]() *OTelHook[S] {
	return &OTelHook[S]{
		tracer:     otel.Tracer("graphflow"),
		graphSpans: make(map[string]trace.Span),
		nodeSpans:  make(map[string]trace.Span),
	}
}

func (h *OTelHook[S]) OnGraphStart(ctx context.Context, graphName string, state S) {
	_, span := h.tracer.Start(ctx, fmt.Sprintf("graph.%s", graphName),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	span.SetAttributes(attribute.String("graph.name", graphName))
	h.graphSpans[graphName] = span
}

func (h *OTelHook[S]) OnGraphEnd(ctx context.Context, graphName string, state S, err error) {
	span, ok := h.graphSpans[graphName]
	if !ok {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
	delete(h.graphSpans, graphName)
}

func (h *OTelHook[S]) OnNodeStart(ctx context.Context, nodeName string, state S) {
	// Find the parent span from context
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	_, nodeSpan := h.tracer.Start(ctx, fmt.Sprintf("graph.node.%s", nodeName),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	nodeSpan.SetAttributes(
		attribute.String("node.name", nodeName),
	)
	h.nodeSpans[nodeName] = nodeSpan
}

func (h *OTelHook[S]) OnNodeEnd(ctx context.Context, nodeName string, state S, err error, duration time.Duration) {
	span, ok := h.nodeSpans[nodeName]
	if !ok {
		return
	}
	span.SetAttributes(
		attribute.Int64("node.duration_ms", duration.Milliseconds()),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
	delete(h.nodeSpans, nodeName)
}

func (h *OTelHook[S]) OnRetry(ctx context.Context, nodeName string, attempt int, lastErr error) {
	span, ok := h.nodeSpans[nodeName]
	if !ok {
		return
	}
	span.AddEvent("retry",
		trace.WithAttributes(
			attribute.Int("retry.attempt", attempt),
			attribute.String("retry.error", lastErr.Error()),
		),
	)
}
