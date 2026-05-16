package graph

import (
	"context"
	"sync"
)

// Stream is a generic, closeable channel wrapper for streaming node output.
type Stream[T any] struct {
	ch   chan T
	done chan struct{}
	mu   sync.Mutex
	err  error
}

// NewStream creates a Stream with the given channel buffer size.
func NewStream[T any](buf int) *Stream[T] {
	return &Stream[T]{
		ch:   make(chan T, buf),
		done: make(chan struct{}),
	}
}

// Send sends a value into the stream. Returns false if the stream is already closed.
func (s *Stream[T]) Send(v T) bool {
	select {
	case <-s.done:
		return false
	case s.ch <- v:
		return true
	}
}

// Chan returns the read-only channel consumers read from.
func (s *Stream[T]) Chan() <-chan T { return s.ch }

// Err returns the error passed to CloseWithError, or nil for a clean close.
func (s *Stream[T]) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close closes the stream normally.
func (s *Stream[T]) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
	default:
		close(s.done)
		close(s.ch)
	}
}

// CloseWithError closes the stream and records an error retrievable via Err.
func (s *Stream[T]) CloseWithError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	select {
	case <-s.done:
	default:
		close(s.done)
		close(s.ch)
	}
}

// Merge fans-in multiple channels into a single Stream.
// The returned stream closes once all inputs are drained or ctx is cancelled.
func Merge[T any](ctx context.Context, streams ...<-chan T) *Stream[T] {
	out := NewStream[T](0)
	var wg sync.WaitGroup
	for _, in := range streams {
		wg.Add(1)
		go func(in <-chan T) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case v, ok := <-in:
					if !ok {
						return
					}
					if !out.Send(v) {
						return
					}
				}
			}
		}(in)
	}
	go func() {
		wg.Wait()
		out.Close()
	}()
	return out
}

// Broadcast fans-out one channel into n Streams.
// Each stream receives every value; all streams close when in closes or ctx is cancelled.
func Broadcast[T any](ctx context.Context, in <-chan T, n int) []*Stream[T] {
	outs := make([]*Stream[T], n)
	for i := range outs {
		outs[i] = NewStream[T](0)
	}
	go func() {
		defer func() {
			for _, o := range outs {
				o.Close()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case v, ok := <-in:
				if !ok {
					return
				}
				for _, o := range outs {
					if !o.Send(v) {
						return
					}
				}
			}
		}
	}()
	return outs
}
