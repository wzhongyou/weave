package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// Middleware wraps an http.Handler with cross-cutting behaviour.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares in order (first wraps second, etc.).
func Chain(handler http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		handler = mws[i](handler)
	}
	return handler
}

// ── RequestID ─────────────────────────────────────────────────────────────────

// RequestIDMiddleware reads X-Request-ID from the incoming request or generates
// a new one, stores it in the context, and sets it on the response header.
func RequestIDMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = newRequestID()
			}
			w.Header().Set("X-Request-ID", id)
			ctx := contextWithRequestID(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Recovery ──────────────────────────────────────────────────────────────────

// RecoveryMiddleware catches panics in downstream handlers, logs them,
// records the metric, and returns a 500 response.
func RecoveryMiddleware(logger *slog.Logger, m *Metrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					m.PanicRecovered()
					stack := debug.Stack()
					logger.Error("panic recovered",
						"request_id", RequestID(r.Context()),
						"panic", rec,
						"stack", string(stack),
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error":"internal server error"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ── Access Logging ────────────────────────────────────────────────────────────

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
	written int64
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += int64(n)
	return n, err
}

// AccessLogMiddleware logs every request in structured format.
func AccessLogMiddleware(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rw, r)

			logger.Info("access",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"bytes", rw.written,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestID(r.Context()),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

// ── Timeout ───────────────────────────────────────────────────────────────────

// TimeoutMiddleware wraps each request with a context deadline.
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── Concurrency Limit ─────────────────────────────────────────────────────────

// ConcurrencyLimitMiddleware limits the number of concurrent requests.
// When the limit is reached, subsequent requests receive 503 Service Unavailable.
func ConcurrencyLimitMiddleware(limit int) Middleware {
	if limit <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	sem := make(chan struct{}, limit)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"error":"server overloaded; try again later"}`))
			}
		})
	}
}
