package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// HTTPServer exposes graph execution via REST endpoints with production-grade
// observability (structured logging, metrics, request ID, panic recovery).
type HTTPServer struct {
	cfg     Config
	graphs  map[string]GraphHandler
	metrics *Metrics
	mux     *http.ServeMux
	srv     *http.Server
}

// NewHTTPServer creates an HTTP server with the given configuration.
func NewHTTPServer(cfg Config) *HTTPServer {
	cfg.defaults()

	mux := http.NewServeMux()
	m := NewMetrics()

	s := &HTTPServer{
		cfg:     cfg,
		graphs:  make(map[string]GraphHandler),
		metrics: m,
		mux:     mux,
	}

	// Register built-in endpoints.
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", m.MetricsHandler().ServeHTTP)
	mux.HandleFunc("/", s.handleRun)

	return s
}

// Register registers a named graph handler.
func (s *HTTPServer) Register(name string, handler GraphHandler) {
	s.graphs[name] = handler
	s.cfg.Logger.Info("graph registered", "name", name)
}

// Metrics returns the server's metrics collector.
func (s *HTTPServer) Metrics() *Metrics { return s.metrics }

// Handler returns the http.Handler wrapped with production middleware.
// This is the handler assigned to http.Server.Handler.
func (s *HTTPServer) Handler() http.Handler {
	return s.buildMiddlewareChain(s.mux)
}

// buildMiddlewareChain wraps the core mux with production middlewares.
func (s *HTTPServer) buildMiddlewareChain(handler http.Handler) http.Handler {
	mws := []Middleware{
		RequestIDMiddleware(),
		RecoveryMiddleware(s.cfg.Logger, s.metrics),
		AccessLogMiddleware(s.cfg.Logger),
	}

	if s.cfg.MaxConcurrent > 0 {
		mws = append(mws, ConcurrencyLimitMiddleware(s.cfg.MaxConcurrent))
	}

	if s.cfg.ReadTimeout > 0 {
		mws = append(mws, TimeoutMiddleware(s.cfg.ReadTimeout))
	}

	return Chain(handler, mws...)
}

// ListenAndServe starts the HTTP server and blocks until the context is cancelled.
// Performs graceful shutdown, waiting up to ShutdownTimeout for in-flight requests.
func (s *HTTPServer) ListenAndServe(ctx context.Context) error {
	addr := s.cfg.Addr
	s.srv = &http.Server{
		Addr:         addr,
		Handler:      s.buildMiddlewareChain(s.mux),
		ReadTimeout:  s.cfg.ReadTimeout,
		WriteTimeout: s.cfg.WriteTimeout,
		IdleTimeout:  120 * time.Second,
	}

	s.cfg.Logger.Info("http server starting",
		"addr", addr,
		"tls", s.cfg.TLSCertFile != "",
		"max_concurrent", s.cfg.MaxConcurrent,
		"max_message_size", s.cfg.MaxMessageSize,
	)

	errCh := make(chan error, 1)
	go func() {
		if s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "" {
			s.srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			errCh <- s.srv.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		} else {
			errCh <- s.srv.ListenAndServe()
		}
	}()

	select {
	case <-ctx.Done():
		s.cfg.Logger.Info("http server shutting down", "timeout", s.cfg.ShutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	s.metrics.RequestBegin()
	defer func() { s.metrics.RequestEnd(status, 0) }()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	snap := s.metrics.Snapshot()
	json.NewEncoder(w).Encode(map[string]any{
		"status":         "ok",
		"uptime_seconds": snap.UptimeSeconds,
		"graphs":         len(s.graphs),
	})
}

func (s *HTTPServer) handleRun(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	s.metrics.RequestBegin()
	defer func() { s.metrics.RequestEnd(status, time.Since(time.Now())) }()

	if r.Method != http.MethodPost {
		status = http.StatusMethodNotAllowed
		s.writeError(w, r, status, "only POST is supported")
		return
	}

	// Extract graph name from path: /{graphName}
	graphName := strings.TrimPrefix(r.URL.Path, "/")
	if graphName == "" || graphName == "health" || graphName == "metrics" {
		status = http.StatusBadRequest
		s.writeError(w, r, status, "graph name is required in path")
		return
	}

	handler, ok := s.graphs[graphName]
	if !ok {
		status = http.StatusNotFound
		s.writeError(w, r, status, fmt.Sprintf("graph %q not found", graphName))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, int64(s.cfg.MaxMessageSize)))
	if err != nil {
		status = http.StatusBadRequest
		s.writeError(w, r, status, fmt.Sprintf("failed to read body: %v", err))
		return
	}

	start := time.Now()
	result, err := handler(r.Context(), body)
	duration := time.Since(start)

	requestID := RequestID(r.Context())

	if err != nil {
		status = http.StatusInternalServerError
		s.metrics.GraphExecuted(false)
		s.cfg.Logger.Warn("graph execution failed",
			"request_id", requestID,
			"graph", graphName,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
		s.writeError(w, r, status, err.Error())
		return
	}

	s.metrics.GraphExecuted(true)
	s.cfg.Logger.Info("graph executed",
		"request_id", requestID,
		"graph", graphName,
		"duration_ms", duration.Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(result)
}

func (s *HTTPServer) writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	// Update the deferred RequestEnd with the actual error status.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:     msg,
		RequestID: RequestID(r.Context()),
	})
}

// Ensure slog is referenced (used in middleware.go).
var _ = slog.LevelDebug
