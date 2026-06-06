package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

// TCPServer exposes graph execution via raw TCP connections using
// newline-delimited JSON (NDJSON).
type TCPServer struct {
	cfg         Config
	graphs      map[string]GraphHandler
	metrics     *Metrics
	ln          net.Listener
	activeConns atomic.Int64
}

// NewTCPServer creates a TCP server with the given configuration.
func NewTCPServer(cfg Config) *TCPServer {
	cfg.defaults()

	return &TCPServer{
		cfg:     cfg,
		graphs:  make(map[string]GraphHandler),
		metrics: NewMetrics(),
	}
}

// Register registers a named graph handler.
func (s *TCPServer) Register(name string, handler GraphHandler) {
	s.graphs[name] = handler
	s.cfg.Logger.Info("tcp graph registered", "name", name)
}

// Metrics returns the server's metrics collector.
func (s *TCPServer) Metrics() *Metrics { return s.metrics }

// ListenAndServe starts the TCP server and blocks until the context is cancelled.
func (s *TCPServer) ListenAndServe(ctx context.Context) error {
	var err error
	s.ln, err = net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("tcp listen: %w", err)
	}

	s.cfg.Logger.Info("tcp server starting",
		"addr", s.cfg.Addr,
		"max_message_size", s.cfg.MaxMessageSize,
	)

	errCh := make(chan error, 1)
	go func() {
		for {
			conn, err := s.ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					errCh <- nil
				default:
					errCh <- err
				}
				return
			}
			s.activeConns.Add(1)
			go func() {
				defer s.activeConns.Add(-1)
				s.handleConn(ctx, conn)
			}()
		}
	}()

	select {
	case <-ctx.Done():
		s.cfg.Logger.Info("tcp server shutting down",
			"active_connections", s.activeConns.Load())
		s.ln.Close()
		return nil
	case err := <-errCh:
		return err
	}
}

// tcpRequest is the wire format for a TCP graph execution request.
type tcpRequest struct {
	Graph string          `json:"graph"`
	State json.RawMessage `json:"state"`
}

func (s *TCPServer) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	logger := s.cfg.Logger.With("remote_addr", remoteAddr)
	logger.Debug("tcp connection accepted")

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), s.cfg.MaxMessageSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		s.metrics.RequestBegin()

		var req tcpRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.metrics.RequestEnd(400, 0)
			logger.Warn("tcp invalid request", "error", err)
			fmt.Fprintf(conn, `{"error":"invalid request: %v"}`+"\n", err)
			continue
		}

		handler, ok := s.graphs[req.Graph]
		if !ok {
			s.metrics.RequestEnd(404, 0)
			logger.Warn("tcp unknown graph", "graph", req.Graph)
			fmt.Fprintf(conn, `{"error":"graph %q not found"}`+"\n", req.Graph)
			continue
		}

		if s.cfg.ReadTimeout > 0 {
			conn.SetDeadline(time.Now().Add(s.cfg.ReadTimeout))
		}

		start := time.Now()
		result, err := handler(ctx, req.State)
		duration := time.Since(start)

		if err != nil {
			s.metrics.RequestEnd(500, duration)
			s.metrics.GraphExecuted(false)
			logger.Warn("tcp graph execution failed",
				"graph", req.Graph,
				"duration_ms", duration.Milliseconds(),
				"error", err,
			)
			fmt.Fprintf(conn, `{"error":%q}`+"\n", err.Error())
			continue
		}

		s.metrics.RequestEnd(200, duration)
		s.metrics.GraphExecuted(true)
		logger.Info("tcp graph executed",
			"graph", req.Graph,
			"duration_ms", duration.Milliseconds(),
		)

		if s.cfg.WriteTimeout > 0 {
			conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
		}

		fmt.Fprintf(conn, "%s\n", result)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		logger.Warn("tcp read error", "error", err)
		fmt.Fprintf(conn, `{"error":"read error: %v"}`+"\n", err)
	}
}

