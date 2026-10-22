// Package capture keeps what the mocks received, in memory only.
//
// Each kind of message (SMS, WhatsApp, push, ...) has its own fixed-size ring
// buffer. When a ring is full the oldest entry is dropped. Everything is lost
// when the process stops, on purpose: this is a local QA tool, not a store.
package capture

import "sync"

// Ring is a fixed-capacity ring buffer that is safe for concurrent use.
type Ring[T any] struct {
	mu   sync.RWMutex
	buf  []T
	next int // index the next Add writes to
	n    int // number of valid entries (<= len(buf))
}

// NewRing returns a ring that holds at most capacity entries (minimum 1).
func NewRing[T any](capacity int) *Ring[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring[T]{buf: make([]T, capacity)}
}

// Add stores v, dropping the oldest entry when the ring is full. Every string
// in v is clipped to MaxField bytes first.
func (r *Ring[T]) Add(v T) {
	v = clip(v)
	r.mu.Lock()
	r.buf[r.next] = v
	r.next = (r.next + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
	r.mu.Unlock()
}

// List returns the entries newest first. keep may be nil; otherwise only
// entries for which keep returns true are returned. limit <= 0 means no limit.
func (r *Ring[T]) List(keep func(T) bool, limit int) []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]T, 0, r.n)
	for i := 1; i <= r.n; i++ {
		v := r.buf[(r.next-i+len(r.buf))%len(r.buf)]
		if keep != nil && !keep(v) {
			continue
		}
		out = append(out, v)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Len reports how many entries the ring holds.
func (r *Ring[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.n
}

// Cap reports the ring's capacity.
func (r *Ring[T]) Cap() int { return len(r.buf) }

// Clear drops every entry.
func (r *Ring[T]) Clear() {
	r.mu.Lock()
	clear(r.buf)
	r.next, r.n = 0, 0
	r.mu.Unlock()
}
