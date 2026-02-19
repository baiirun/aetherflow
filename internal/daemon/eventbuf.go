package daemon

import (
	"encoding/json"
	"sync"
)

// DefaultEventBufSize is the maximum number of events stored per agent.
// Oldest events are evicted when this limit is exceeded.
const DefaultEventBufSize = 2000

// SessionEvent is a single event received from the opencode plugin.
// The Data field carries the raw event properties as-is from the plugin —
// the daemon parses what it needs and stores the rest opaquely.
type SessionEvent struct {
	AgentID   string          `json:"agent_id"`
	EventType string          `json:"event_type"`
	SessionID string          `json:"session_id,omitempty"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// EventBuffer stores events per agent in bounded ring buffers.
// It is safe for concurrent use.
type EventBuffer struct {
	mu      sync.RWMutex
	agents  map[string]*agentBuf
	maxSize int
}

type agentBuf struct {
	events []SessionEvent
}

// NewEventBuffer creates a new event buffer with the given per-agent capacity.
func NewEventBuffer(maxSize int) *EventBuffer {
	if maxSize <= 0 {
		maxSize = DefaultEventBufSize
	}
	return &EventBuffer{
		agents:  make(map[string]*agentBuf),
		maxSize: maxSize,
	}
}

// Push appends an event to the agent's buffer, evicting the oldest
// event if the buffer is at capacity.
func (b *EventBuffer) Push(ev SessionEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	buf, ok := b.agents[ev.AgentID]
	if !ok {
		buf = &agentBuf{events: make([]SessionEvent, 0, 64)}
		b.agents[ev.AgentID] = buf
	}

	if len(buf.events) >= b.maxSize {
		// Drop oldest event. This is O(n) but maxSize is bounded (2000)
		// and pushes are infrequent relative to CPU cost.
		copy(buf.events, buf.events[1:])
		buf.events[len(buf.events)-1] = ev
	} else {
		buf.events = append(buf.events, ev)
	}
}

// Events returns all events for the given agent, oldest first.
// Returns nil if no events exist for the agent.
func (b *EventBuffer) Events(agentID string) []SessionEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	buf, ok := b.agents[agentID]
	if !ok || len(buf.events) == 0 {
		return nil
	}

	// Return a copy so callers don't hold the lock.
	out := make([]SessionEvent, len(buf.events))
	copy(out, buf.events)
	return out
}

// EventsSince returns events for the given agent with timestamps strictly
// after the given timestamp, oldest first. Useful for incremental reads.
func (b *EventBuffer) EventsSince(agentID string, afterTimestamp int64) []SessionEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	buf, ok := b.agents[agentID]
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

// Clear removes all events for the given agent.
func (b *EventBuffer) Clear(agentID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.agents, agentID)
}

// Len returns the number of events stored for the given agent.
func (b *EventBuffer) Len(agentID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	buf, ok := b.agents[agentID]
	if !ok {
		return 0
	}
	return len(buf.events)
}
