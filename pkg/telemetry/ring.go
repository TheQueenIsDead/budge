package telemetry

import "sync"

// ring keeps the most recent items up to its capacity, dropping the oldest
// as new ones arrive. Telemetry is kept since the process started, but only
// so much of it, so memory stays flat however long budge runs.
type ring[T any] struct {
	mu    sync.RWMutex
	items []T
	next  int
	full  bool
}

func newRing[T any](capacity int) *ring[T] {
	return &ring[T]{items: make([]T, capacity)}
}

func (r *ring[T]) add(items ...T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range items {
		r.items[r.next] = item
		r.next = (r.next + 1) % len(r.items)
		if r.next == 0 {
			r.full = true
		}
	}
}

// all returns what is kept, newest first.
func (r *ring[T]) all() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := r.next
	if r.full {
		n = len(r.items)
	}
	out := make([]T, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, r.items[(r.next-i+len(r.items))%len(r.items)])
	}
	return out
}
