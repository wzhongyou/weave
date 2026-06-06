// Package server provides industrial-grade HTTP and TCP servers that expose
// graph execution as network-accessible services.
//
// Features:
//   - Structured logging (log/slog)
//   - Request metrics (counters, latency, in-flight)
//   - Request ID propagation
//   - Panic recovery
//   - Graceful shutdown with drain timeout
//   - TLS support
//   - Concurrent request limiting
package server

import (
	"context"
	"log/slog"
	"time"
)

// GraphHandler is a type-erased graph execution handler.
// Input and output are wire-format bytes (typically JSON).
type GraphHandler func(ctx context.Context, input []byte) ([]byte, error)

// Config holds server configuration.
type Config struct {
	// Addr is the listen address (e.g. ":8080", ":9090").
	Addr string

	// MaxMessageSize is the maximum request body size in bytes.
	// Defaults to 1 MiB.
	MaxMessageSize int

	// ReadTimeout is the per-connection read deadline.
	ReadTimeout time.Duration

	// WriteTimeout is the per-connection write deadline.
	WriteTimeout time.Duration

	// ShutdownTimeout is the maximum time to wait for in-flight requests
	// during graceful shutdown. Defaults to 30s.
	ShutdownTimeout time.Duration

	// MaxConcurrent limits the number of concurrent graph executions.
	// 0 = unlimited.
	MaxConcurrent int

	// TLSCertFile and TLSKeyFile enable HTTPS when both are set.
	TLSCertFile string
	TLSKeyFile  string

	// Logger receives structured logs. Defaults to slog.Default().
	Logger *slog.Logger
}

// ErrorResponse is the standard JSON error envelope.
type ErrorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

func (c *Config) defaults() {
	if c.MaxMessageSize == 0 {
		c.MaxMessageSize = 1 << 20 // 1 MiB
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 30 * time.Second
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// requestIDKey is the context key for the request ID.
type requestIDKey struct{}

// RequestID extracts the request ID from the context, or returns "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// contextWithRequestID stores the request ID in the context.
func contextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}
