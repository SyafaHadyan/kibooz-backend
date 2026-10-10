// Package ratelimit counts requests in sliding windows. The counters sit behind a Store, and the one that Redis backs
// updates them with atomic commands, so any number of API instances that share a Redis share one limit.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Store counts one request under a key and tells how many requests it has seen lately
type Store interface {
	// Hit counts one request under key in the current window of the given length. The windows are the consecutive
	// stretches of that length since the Unix epoch, so every instance agrees on where a window starts.
	Hit(ctx context.Context, key string, window time.Duration) (Hits, error)
}

// Hits is what a Store knows after it counted a request
type Hits struct {
	// Current is the number of requests in the current window, with this one
	Current int64
	// Previous is the number of requests in the window before it
	Previous int64
	// Elapsed is how far into the current window the request is
	Elapsed time.Duration
}

// Estimate is the number of requests in the sliding window that ends now. The previous window counts for the part of it
// that the sliding window still covers, rounded up. Rounding up means that requests spread over a window boundary are
// never counted as fewer than they are while they are close together, so a client cannot slip one request more than the
// limit through at the start of a window. It also never refuses a client whose requests in both windows add up to the
// limit or less, because the share of the previous window is never larger than the previous window itself.
func (h Hits) Estimate(window time.Duration) int64 {
	remaining := int64(window - h.Elapsed)
	length := int64(window)

	return (h.Previous*remaining+length-1)/length + h.Current
}

// Window returns the number of the window that a moment falls in and how far into it the moment is
func Window(now time.Time, window time.Duration) (index int64, elapsed time.Duration) {
	nanos := now.UnixNano()
	length := int64(window)

	return nanos / length, time.Duration(nanos % length)
}

// Memory counts in the memory of the process. It is the store of a single instance, and the fallback of the one that
// Redis backs while Redis is down.
type Memory struct {
	mu      sync.Mutex
	entries map[string]*counter
	now     func() time.Time

	// nextPrune is the earliest moment of the next sweep, so a map full of live keys is not scanned by every request
	nextPrune time.Time
}

type counter struct {
	index    int64
	current  int64
	previous int64
	expires  time.Time
}

// pruneAbove is the number of keys after which expired ones are dropped, to keep the memory bounded
const pruneAbove = 10000

func NewMemory() *Memory {
	return NewMemoryWithClock(time.Now)
}

// NewMemoryWithClock reads the time from now, so a test can move it
func NewMemoryWithClock(now func() time.Time) *Memory {
	return &Memory{entries: make(map[string]*counter), now: now}
}

// Len is the number of keys that the store holds
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.entries)
}

func (m *Memory) Hit(_ context.Context, key string, window time.Duration) (Hits, error) {
	now := m.now()
	index, elapsed := Window(now, window)

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.entries) > pruneAbove && !now.Before(m.nextPrune) {
		m.nextPrune = now.Add(window)

		for k, entry := range m.entries {
			if now.After(entry.expires) {
				delete(m.entries, k)
			}
		}
	}

	entry, found := m.entries[key]

	switch {
	case !found:
		entry = &counter{index: index}
		m.entries[key] = entry
	case entry.index == index-1:
		entry.previous, entry.current, entry.index = entry.current, 0, index
	case entry.index != index:
		entry.previous, entry.current, entry.index = 0, 0, index
	}

	entry.current++
	entry.expires = now.Add(2 * window)

	return Hits{Current: entry.current, Previous: entry.previous, Elapsed: elapsed}, nil
}
