// Package logbuffer provides a thread-safe in-memory ring buffer for log
// entries with SSE fan-out support. The buffer holds up to Cap entries;
// older entries are overwritten once full.
package logbuffer

import (
	"sync"
	"sync/atomic"
	"time"
)

// Cap is the maximum number of log entries retained in memory.
const Cap = 500

// LogEntry is a single structured log line captured from the application.
type LogEntry struct {
	ID      uint64         `json:"id"`
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// Buffer is a bounded, thread-safe ring buffer of LogEntry values with
// support for multiple concurrent SSE subscribers.
type Buffer struct {
	mu          sync.RWMutex
	entries     [Cap]LogEntry
	head        int // index of the next write slot (mod Cap)
	count       int // number of valid entries (capped at Cap)
	counter     atomic.Uint64
	subsMu      sync.Mutex
	subscribers map[uint64]chan LogEntry
	subCounter  atomic.Uint64
}

// NewBuffer allocates and returns a ready-to-use Buffer.
func NewBuffer() *Buffer {
	return &Buffer{
		subscribers: make(map[uint64]chan LogEntry),
	}
}

// Append writes a log entry to the ring buffer and fans it out to all
// current SSE subscribers. The entry ID is assigned automatically.
func (b *Buffer) Append(level, message string, fields map[string]any) {
	e := LogEntry{
		ID:      b.counter.Add(1),
		Time:    time.Now(),
		Level:   level,
		Message: message,
		Fields:  fields,
	}

	b.mu.Lock()
	b.entries[b.head] = e
	b.head = (b.head + 1) % Cap
	if b.count < Cap {
		b.count++
	}
	b.mu.Unlock()

	// Fan-out to SSE subscribers (non-blocking per subscriber).
	b.subsMu.Lock()
	for _, ch := range b.subscribers {
		select {
		case ch <- e:
		default:
			// subscriber is slow; drop rather than block
		}
	}
	b.subsMu.Unlock()
}

// Entries returns all buffered entries whose ID is greater than sinceID,
// in chronological order. Pass sinceID=0 to retrieve all entries.
func (b *Buffer) Entries(sinceID uint64) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.count == 0 {
		return nil
	}

	result := make([]LogEntry, 0, b.count)
	start := (b.head - b.count + Cap) % Cap
	for i := 0; i < b.count; i++ {
		e := b.entries[(start+i)%Cap]
		if e.ID > sinceID {
			result = append(result, e)
		}
	}
	return result
}

// Subscribe registers a new SSE subscriber. It returns a read-only channel
// that will receive future log entries, and an unsubscribe function that
// must be called when the subscriber disconnects.
func (b *Buffer) Subscribe() (<-chan LogEntry, func()) {
	id := b.subCounter.Add(1)
	ch := make(chan LogEntry, 64)

	b.subsMu.Lock()
	b.subscribers[id] = ch
	b.subsMu.Unlock()

	unsub := func() {
		b.subsMu.Lock()
		delete(b.subscribers, id)
		b.subsMu.Unlock()
		close(ch)
	}
	return ch, unsub
}
