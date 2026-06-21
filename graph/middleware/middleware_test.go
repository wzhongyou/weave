package middleware_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wzhongyou/weave/graph"
	"github.com/wzhongyou/weave/graph/middleware"
)

type S struct{ val int }

func fn(out int) graph.NodeFunc[*S] {
	return func(_ context.Context, _ *S) (*S, error) { return &S{val: out}, nil }
}

func errFn(err error) graph.NodeFunc[*S] {
	return func(_ context.Context, s *S) (*S, error) { return s, err }
}

// ── retry ─────────────────────────────────────────────────────────────────────

func TestRetry_SuccessOnSecondAttempt(t *testing.T) {
	calls := 0
	flaky := func(_ context.Context, s *S) (*S, error) {
		calls++
		if calls < 2 {
			return s, errors.New("transient")
		}
		return &S{val: 42}, nil
	}

	wrapped := middleware.WithRetry(flaky, middleware.RetryPolicy{MaxAttempts: 3})
	res, err := wrapped(context.Background(), &S{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.val != 42 {
		t.Errorf("want 42, got %d", res.val)
	}
	if calls != 2 {
		t.Errorf("want 2 calls, got %d", calls)
	}
}

func TestRetry_ExhaustsAttempts(t *testing.T) {
	calls := 0
	boom := func(_ context.Context, s *S) (*S, error) {
		calls++
		return s, errors.New("permanent")
	}

	wrapped := middleware.WithRetry(boom, middleware.RetryPolicy{
		MaxAttempts: 3,
		Backoff:     time.Millisecond,
	})
	_, err := wrapped(context.Background(), &S{})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 3 {
		t.Errorf("want 3 calls, got %d", calls)
	}
}

func TestRetry_RetryOnFilter(t *testing.T) {
	permanent := errors.New("permanent")
	calls := 0
	f := func(_ context.Context, s *S) (*S, error) {
		calls++
		return s, permanent
	}

	wrapped := middleware.WithRetry(f, middleware.RetryPolicy{
		MaxAttempts: 5,
		Backoff:     time.Millisecond,
		RetryOn:     func(err error) bool { return false }, // never retry
	})
	_, err := wrapped(context.Background(), &S{})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("want 1 call (no retry), got %d", calls)
	}
}

func TestRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	calls := 0
	f := func(_ context.Context, s *S) (*S, error) {
		calls++
		return s, errors.New("err")
	}
	wrapped := middleware.WithRetry(f, middleware.RetryPolicy{
		MaxAttempts: 10,
		Backoff:     10 * time.Millisecond,
	})
	_, err := wrapped(ctx, &S{})
	if err == nil {
		t.Fatal("expected error")
	}
	// First call executes, then ctx is already done so we stop immediately.
	if calls > 2 {
		t.Errorf("expected at most 2 calls with cancelled context, got %d", calls)
	}
}

// ── circuit breaker ───────────────────────────────────────────────────────────

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenTimeout:      50 * time.Millisecond,
	})
	wrapped := middleware.WithCircuitBreaker(errFn(errors.New("fail")), cb)

	// 3 failures should trip the breaker.
	for i := 0; i < 3; i++ {
		_, _ = wrapped(context.Background(), &S{})
	}

	// Next call must get ErrCircuitOpen.
	_, err := wrapped(context.Background(), &S{})
	if !errors.Is(err, graph.ErrCircuitOpen) {
		t.Errorf("want ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		OpenTimeout:      20 * time.Millisecond,
	})

	fail := middleware.WithCircuitBreaker(errFn(errors.New("fail")), cb)
	succeed := middleware.WithCircuitBreaker(fn(1), cb)

	// Trip the breaker.
	_, _ = fail(context.Background(), &S{})
	_, _ = fail(context.Background(), &S{})

	// Confirm open.
	if _, err := fail(context.Background(), &S{}); !errors.Is(err, graph.ErrCircuitOpen) {
		t.Fatal("expected ErrCircuitOpen")
	}

	// Wait for open timeout → half-open.
	time.Sleep(30 * time.Millisecond)

	// Two successes should close it.
	for i := 0; i < 2; i++ {
		if _, err := succeed(context.Background(), &S{}); err != nil {
			t.Fatalf("unexpected error during recovery: %v", err)
		}
	}

	// Should be closed now — normal call works.
	if _, err := succeed(context.Background(), &S{}); err != nil {
		t.Errorf("unexpected error after recovery: %v", err)
	}
}

// ── bulkhead ──────────────────────────────────────────────────────────────────

func TestBulkhead_RejectsOverflow(t *testing.T) {
	bh := middleware.NewBulkhead(2)
	hold := make(chan struct{})
	slow := func(_ context.Context, s *S) (*S, error) {
		<-hold
		return s, nil
	}
	wrapped := middleware.WithBulkhead(slow, bh)

	// Occupy both slots.
	go wrapped(context.Background(), &S{})
	go wrapped(context.Background(), &S{})
	time.Sleep(10 * time.Millisecond) // let goroutines enter

	// Third call should be rejected.
	_, err := wrapped(context.Background(), &S{})
	close(hold)
	if !errors.Is(err, graph.ErrBulkheadFull) {
		t.Errorf("want ErrBulkheadFull, got %v", err)
	}
}

// ── timeout ───────────────────────────────────────────────────────────────────

func TestTimeout_Fires(t *testing.T) {
	slow := func(ctx context.Context, s *S) (*S, error) {
		select {
		case <-ctx.Done():
			return s, ctx.Err()
		case <-time.After(5 * time.Second):
			return s, nil
		}
	}
	wrapped := middleware.WithTimeout(slow, 30*time.Millisecond)
	_, err := wrapped(context.Background(), &S{})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

// ── rate limit ────────────────────────────────────────────────────────────────

type tokenLimiter struct{ tokens atomic.Int32 }

func (l *tokenLimiter) Wait(_ context.Context) error {
	if l.tokens.Add(-1) < 0 {
		return errors.New("rate limit exceeded")
	}
	return nil
}

func TestRateLimit(t *testing.T) {
	lim := &tokenLimiter{}
	lim.tokens.Store(2)
	wrapped := middleware.WithRateLimit(fn(1), lim)

	// First two calls succeed.
	for i := 0; i < 2; i++ {
		if _, err := wrapped(context.Background(), &S{}); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
	// Third call should be limited.
	if _, err := wrapped(context.Background(), &S{}); err == nil {
		t.Error("expected rate limit error")
	}
}

// ── cache ─────────────────────────────────────────────────────────────────────

type memCache[S any] struct{ m map[string]S }

func newMemCache[S any]() *memCache[S] { return &memCache[S]{m: make(map[string]S)} }

func (c *memCache[S]) Get(_ context.Context, key string) (S, bool, error) {
	v, ok := c.m[key]
	return v, ok, nil
}
func (c *memCache[S]) Set(_ context.Context, key string, state S) error {
	c.m[key] = state
	return nil
}

func TestCache_HitSkipsFn(t *testing.T) {
	calls := 0
	expensive := func(_ context.Context, _ *S) (*S, error) {
		calls++
		return &S{val: 99}, nil
	}
	cache := newMemCache[*S]()
	keyFn := func(_ context.Context, s *S) string { return "fixed-key" }
	wrapped := middleware.WithCache(expensive, cache, keyFn)

	// First call: miss → execute fn.
	res, _ := wrapped(context.Background(), &S{})
	if calls != 1 || res.val != 99 {
		t.Fatalf("unexpected first-call result: calls=%d val=%d", calls, res.val)
	}

	// Second call: hit → fn NOT called again.
	res, _ = wrapped(context.Background(), &S{})
	if calls != 1 {
		t.Errorf("fn should not be called on cache hit, calls=%d", calls)
	}
	if res.val != 99 {
		t.Errorf("want cached 99, got %d", res.val)
	}
}

func TestCache_EmptyKeyBypassesCache(t *testing.T) {
	calls := 0
	f := func(_ context.Context, _ *S) (*S, error) {
		calls++
		return &S{val: calls}, nil
	}
	cache := newMemCache[*S]()
	keyFn := func(_ context.Context, _ *S) string { return "" } // no cache
	wrapped := middleware.WithCache(f, cache, keyFn)

	wrapped(context.Background(), &S{})
	wrapped(context.Background(), &S{})
	if calls != 2 {
		t.Errorf("want 2 calls (no caching), got %d", calls)
	}
}

// ── recover (panic → error) ───────────────────────────────────────────────────

func TestRecover_PanicToError(t *testing.T) {
	panicky := func(_ context.Context, s *S) (*S, error) {
		panic("something went wrong")
	}
	wrapped := middleware.WithRecover[*S]("test-node", panicky)
	_, err := wrapped(context.Background(), &S{})
	if err == nil {
		t.Fatal("expected error from panic recovery")
	}
	var pe *graph.PanicError
	if !errors.As(err, &pe) {
		t.Errorf("want *PanicError, got %T: %v", err, err)
	}
}

// ── validate ──────────────────────────────────────────────────────────────────

func TestValidate_PreConditionFails(t *testing.T) {
	wrapped := middleware.WithValidate(fn(1),
		func(_ context.Context, s *S) error {
			if s.val < 0 {
				return errors.New("negative input")
			}
			return nil
		},
		nil,
	)
	_, err := wrapped(context.Background(), &S{val: -1})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
