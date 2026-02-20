package daemon

import (
	"encoding/json"
	"sync"
	"time"
)

// DefaultEventBufSize is the maximum number of events stored per session.
// Oldest events are evicted when this limit is exceeded.
const DefaultEventBufSize = 2000

// sessionIdleTTL is how long an idle session's events are kept before
// being swept. Matches exitedSpawnTTL so both spawn entries and their
// corresponding event data expire together.
const sessionIdleTTL = 1 * time.Hour

// SessionEvent is a single event received from the opencode plugin.
// The Data field carries the raw event properties as-is from the plugin —
// the daemon parses what it needs and stores the rest opaquely.
type SessionEvent struct {
	EventType string          `json:"event_type"`
	SessionID string          `json:"session_id"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// EventBuffer stores events per session in bounded ring buffers.
// Events are keyed by opencode session ID — the natural identifier
// that appears on every plugin event. Consumers look up the session ID
// from the pool agent or spawn entry, then query events by session ID.
//
// It is safe for concurrent use.
type EventBuffer struct {
	mu       sync.RWMutex
	sessions map[string]*sessionBuf
	maxSize  int
}

type sessionBuf struct {
	events   []SessionEvent
	lastPush time.Time // wall clock of the most recent Push
}

// NewEventBuffer creates a new event buffer with the given per-session capacity.
func NewEventBuffer(maxSize int) *EventBuffer {
	if maxSize <= 0 {
		maxSize = DefaultEventBufSize
	}
	return &EventBuffer{
		sessions: make(map[string]*sessionBuf),
		maxSize:  maxSize,
	}
}

// Push appends an event to the session's buffer, evicting the oldest
// event if the buffer is at capacity.
func (b *EventBuffer) Push(ev SessionEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()

	buf, ok := b.sessions[ev.SessionID]
	if !ok {
		buf = &sessionBuf{events: make([]SessionEvent, 0, 64)}
		b.sessions[ev.SessionID] = buf
	}
	buf.lastPush = now

	if len(buf.events) >= b.maxSize {
		// Drop oldest event. This is O(n) but maxSize is bounded (2000)
		// and pushes are infrequent relative to CPU cost.
		copy(buf.events, buf.events[1:])
		buf.events[len(buf.events)-1] = ev
	} else {
		buf.events = append(buf.events, ev)
	}
}

// Events returns all events for the given session, oldest first.
// Returns nil if no events exist for the session.
func (b *EventBuffer) Events(sessionID string) []SessionEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	buf, ok := b.sessions[sessionID]
	if !ok || len(buf.events) == 0 {
		return nil
	}

	// Return a copy so callers don't hold the lock.
	out := make([]SessionEvent, len(buf.events))
	copy(out, buf.events)
	return out
}

// EventsSince returns events for the given session with timestamps strictly
// after the given timestamp, oldest first. Useful for incremental reads.
func (b *EventBuffer) EventsSince(sessionID string, afterTimestamp int64) []SessionEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	buf, ok := b.sessions[sessionID]
	if !ok || len(buf.events) == 0 {
		return nil
	}

	// Find the first event after the timestamp. Events are ordered by
	// arrival time which is monotonically increasing.
	start := -1
	for i := len(buf.events) - 1; i >= 0; i-- {
		if buf.events[i].Timestamp <= afterTimestamp {
			start = i + 1
			break
		}
	}
	if start == -1 {
		start = 0 // all events are after the timestamp
	}
	if start >= len(buf.events) {
		return nil
	}

	out := make([]SessionEvent, len(buf.events)-start)
	copy(out, buf.events[start:])
	return out
}

// Clear removes all events for the given session.
func (b *EventBuffer) Clear(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, sessionID)
}

// SweepIdle removes sessions that have been idle (no Push) for longer
// than sessionIdleTTL. Returns the number of sessions removed.
// Called periodically by the daemon alongside the spawn sweep.
func (b *EventBuffer) SweepIdle() int {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	removed := 0
	for id, buf := range b.sessions {
		if now.Sub(buf.lastPush) > sessionIdleTTL {
			delete(b.sessions, id)
			removed++
		}
	}
	return removed
}

// SessionCount returns the number of sessions tracked by the buffer.
func (b *EventBuffer) SessionCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.sessions)
}

// Len returns the number of events stored for the given session.
func (b *EventBuffer) Len(sessionID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	buf, ok := b.sessions[sessionID]
	if !ok {
		return 0
	}
	return len(buf.events)
}
