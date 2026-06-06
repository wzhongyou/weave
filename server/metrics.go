package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics collects server-wide operational counters.
// All methods are safe for concurrent use.
type Metrics struct {
	// HTTP counters.
	RequestsTotal    atomic.Int64 // total HTTP requests received
	RequestsInFlight atomic.Int64 // currently executing requests
	ErrorsTotal      atomic.Int64 // requests that resulted in 4xx/5xx
	PanicsTotal      atomic.Int64 // recovered panics

	// Graph execution counters.
	GraphExecutionsTotal atomic.Int64 // total graph runs
	GraphFailuresTotal   atomic.Int64 // graph runs that returned an error

	// Latency sum and count for computing averages.
	latencySumMs atomic.Int64
	latencyCount atomic.Int64

	startTime time.Time
}

// NewMetrics creates a Metrics instance with the start time set to now.
func NewMetrics() *Metrics {
	return &Metrics{startTime: time.Now()}
}

// RequestBegin increments the in-flight counter.
func (m *Metrics) RequestBegin() { m.RequestsInFlight.Add(1) }

// RequestEnd decrements in-flight, increments total, and records latency.
func (m *Metrics) RequestEnd(status int, d time.Duration) {
	m.RequestsInFlight.Add(-1)
	m.RequestsTotal.Add(1)
	m.latencySumMs.Add(int64(d / time.Millisecond))
	m.latencyCount.Add(1)
	if status >= 400 {
		m.ErrorsTotal.Add(1)
	}
}

// GraphExecuted records a graph execution.
func (m *Metrics) GraphExecuted(success bool) {
	m.GraphExecutionsTotal.Add(1)
	if !success {
		m.GraphFailuresTotal.Add(1)
	}
}

// PanicRecovered records a recovered panic.
func (m *Metrics) PanicRecovered() { m.PanicsTotal.Add(1) }

// Snapshot returns a point-in-time copy of the metrics suitable for JSON output.
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		RequestsTotal:        m.RequestsTotal.Load(),
		RequestsInFlight:     m.RequestsInFlight.Load(),
		ErrorsTotal:          m.ErrorsTotal.Load(),
		PanicsTotal:          m.PanicsTotal.Load(),
		GraphExecutionsTotal: m.GraphExecutionsTotal.Load(),
		GraphFailuresTotal:   m.GraphFailuresTotal.Load(),
		AvgLatencyMs:         m.avgLatencyMs(),
		UptimeSeconds:        int(time.Since(m.startTime).Seconds()),
	}
}

func (m *Metrics) avgLatencyMs() float64 {
	count := m.latencyCount.Load()
	if count == 0 {
		return 0
	}
	return float64(m.latencySumMs.Load()) / float64(count)
}

// MetricsSnapshot is a point-in-time view of the server metrics.
type MetricsSnapshot struct {
	RequestsTotal        int64   `json:"requests_total"`
	RequestsInFlight     int64   `json:"requests_in_flight"`
	ErrorsTotal          int64   `json:"errors_total"`
	PanicsTotal          int64   `json:"panics_total"`
	GraphExecutionsTotal int64   `json:"graph_executions_total"`
	GraphFailuresTotal   int64   `json:"graph_failures_total"`
	AvgLatencyMs         float64 `json:"avg_latency_ms"`
	UptimeSeconds        int     `json:"uptime_seconds"`
}

// MetricsHandler returns an http.Handler that serves the current metrics as JSON.
func (m *Metrics) MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(m.Snapshot())
	})
}

// writeMetricsError writes a JSON error to an http.ResponseWriter.
func writeMetricsError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
