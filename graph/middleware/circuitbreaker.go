package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/wzhongyou/weave/graph"
)

// CBState represents the circuit breaker FSM state.
type CBState int

const (
	CBClosed   CBState = iota // normal operation
	CBOpen                    // failing; fast-fail all calls
	CBHalfOpen                // probe: allow one call through
)

// CircuitBreakerConfig configures the circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int           // consecutive failures to trip open (default 5)
	SuccessThreshold int           // consecutive successes to close from half-open (default 2)
	OpenTimeout      time.Duration // time to wait before probing (default 10s)
}

func (c *CircuitBreakerConfig) failureThreshold() int {
	if c.FailureThreshold <= 0 {
		return 5
	}
	return c.FailureThreshold
}

func (c *CircuitBreakerConfig) successThreshold() int {
	if c.SuccessThreshold <= 0 {
		return 2
	}
	return c.SuccessThreshold
}

func (c *CircuitBreakerConfig) openTimeout() time.Duration {
	if c.OpenTimeout <= 0 {
		return 10 * time.Second
	}
	return c.OpenTimeout
}

// CircuitBreaker holds the FSM state shared across invocations of the same node.
// Create one per node and reuse it.
type CircuitBreaker struct {
	cfg      CircuitBreakerConfig
	mu       sync.Mutex
	state    CBState
	failures int
	success  int
	openedAt time.Time
	probing  bool // one probe allowed at a time in HalfOpen
}

// NewCircuitBreaker creates a CircuitBreaker with the given config.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{cfg: cfg}
}

// WithCircuitBreaker wraps fn with cb.
// Returns graph.ErrCircuitOpen immediately when the breaker is open.
func WithCircuitBreaker[S any](fn graph.NodeFunc[S], cb *CircuitBreaker) graph.NodeFunc[S] {
	return func(ctx context.Context, state S) (S, error) {
		if err := cb.allow(); err != nil {
			var zero S
			return zero, err
		}
		s, err := fn(ctx, state)
		cb.record(err)
		return s, err
	}
}

// allow checks whether a call may proceed.
func (cb *CircuitBreaker) allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CBOpen {
		if time.Since(cb.openedAt) < cb.cfg.openTimeout() {
			return graph.ErrCircuitOpen
		}
		// OpenTimeout elapsed — transition to HalfOpen for a probe.
		cb.state = CBHalfOpen
		cb.success = 0
		cb.probing = false
	}

	if cb.state == CBHalfOpen {
		if cb.probing {
			return graph.ErrCircuitOpen // only one probe at a time
		}
		cb.probing = true
	}

	return nil
}

// record updates the FSM based on the outcome of the most recent call.
func (cb *CircuitBreaker) record(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CBClosed:
		if err != nil {
			cb.failures++
			if cb.failures >= cb.cfg.failureThreshold() {
				cb.state = CBOpen
				cb.openedAt = time.Now()
			}
		} else {
			cb.failures = 0
		}

	case CBHalfOpen:
		cb.probing = false
		if err != nil {
			cb.state = CBOpen
			cb.openedAt = time.Now()
			cb.failures = 0
			cb.success = 0
		} else {
			cb.success++
			if cb.success >= cb.cfg.successThreshold() {
				cb.state = CBClosed
				cb.failures = 0
				cb.success = 0
			}
		}
	}
}
