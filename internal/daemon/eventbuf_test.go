package daemon

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestEventBufferPushAndEvents(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{AgentID: "agent-1", EventType: "session.created", Timestamp: 1})
	buf.Push(SessionEvent{AgentID: "agent-1", EventType: "message.updated", Timestamp: 2})

	events := buf.Events("agent-1")
	if len(events) != 2 {
		t.Fatalf("Events returned %d events, want 2", len(events))
	}
	if events[0].EventType != "session.created" {
		t.Errorf("events[0].EventType = %q, want %q", events[0].EventType, "session.created")
	}
	if events[1].EventType != "message.updated" {
		t.Errorf("events[1].EventType = %q, want %q", events[1].EventType, "message.updated")
	}
}

func TestEventBufferEventsReturnsNilForUnknownAgent(t *testing.T) {
	buf := NewEventBuffer(100)

	events := buf.Events("nonexistent")
	if events != nil {
		t.Errorf("Events returned %v for unknown agent, want nil", events)
	}
}

func TestEventBufferEventsReturnsCopy(t *testing.T) {
	buf := NewEventBuffer(100)
	buf.Push(SessionEvent{AgentID: "agent-1", EventType: "original", Timestamp: 1})

	events := buf.Events("agent-1")
	events[0].EventType = "modified"

	original := buf.Events("agent-1")
	if original[0].EventType != "original" {
		t.Error("Events should return a copy, but modification affected the original")
	}
}

func TestEventBufferEvictsOldestWhenFull(t *testing.T) {
	buf := NewEventBuffer(3)

	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-1", Timestamp: 1})
	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-2", Timestamp: 2})
	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-3", Timestamp: 3})
	// Buffer is now full. Next push should evict ev-1.
	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-4", Timestamp: 4})

	events := buf.Events("a")
	if len(events) != 3 {
		t.Fatalf("Events returned %d events, want 3", len(events))
	}
	if events[0].EventType != "ev-2" {
		t.Errorf("events[0].EventType = %q, want %q (oldest should be evicted)", events[0].EventType, "ev-2")
	}
	if events[2].EventType != "ev-4" {
		t.Errorf("events[2].EventType = %q, want %q", events[2].EventType, "ev-4")
	}
}

func TestEventBufferIsolatesAgents(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{AgentID: "agent-1", EventType: "a1-event", Timestamp: 1})
	buf.Push(SessionEvent{AgentID: "agent-2", EventType: "a2-event", Timestamp: 2})

	a1 := buf.Events("agent-1")
	a2 := buf.Events("agent-2")

	if len(a1) != 1 || a1[0].EventType != "a1-event" {
		t.Errorf("agent-1 events = %v, want [{EventType: a1-event}]", a1)
	}
	if len(a2) != 1 || a2[0].EventType != "a2-event" {
		t.Errorf("agent-2 events = %v, want [{EventType: a2-event}]", a2)
	}
}

func TestEventBufferEventsSince(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-1", Timestamp: 100})
	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-2", Timestamp: 200})
	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-3", Timestamp: 300})
	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-4", Timestamp: 400})

	// Events strictly after timestamp 200.
	events := buf.EventsSince("a", 200)
	if len(events) != 2 {
		t.Fatalf("EventsSince(200) returned %d events, want 2", len(events))
	}
	if events[0].EventType != "ev-3" {
		t.Errorf("events[0].EventType = %q, want %q", events[0].EventType, "ev-3")
	}
	if events[1].EventType != "ev-4" {
		t.Errorf("events[1].EventType = %q, want %q", events[1].EventType, "ev-4")
	}
}

func TestEventBufferEventsSinceAllAfter(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-1", Timestamp: 100})
	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-2", Timestamp: 200})

	// All events are after timestamp 0.
	events := buf.EventsSince("a", 0)
	if len(events) != 2 {
		t.Fatalf("EventsSince(0) returned %d events, want 2", len(events))
	}
}

func TestEventBufferEventsSinceNoneAfter(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{AgentID: "a", EventType: "ev-1", Timestamp: 100})

	events := buf.EventsSince("a", 100)
	if events != nil {
		t.Errorf("EventsSince(100) returned %v, want nil (no events strictly after 100)", events)
	}
}

func TestEventBufferEventsSinceUnknownAgent(t *testing.T) {
	buf := NewEventBuffer(100)

	events := buf.EventsSince("nonexistent", 0)
	if events != nil {
		t.Errorf("EventsSince for unknown agent returned %v, want nil", events)
	}
}

func TestEventBufferClear(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{AgentID: "a", EventType: "ev", Timestamp: 1})
	buf.Clear("a")

	if buf.Len("a") != 0 {
		t.Errorf("Len after Clear = %d, want 0", buf.Len("a"))
	}
	if buf.Events("a") != nil {
		t.Errorf("Events after Clear = %v, want nil", buf.Events("a"))
	}
}

func TestEventBufferClearDoesNotAffectOtherAgents(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{AgentID: "a", EventType: "ev", Timestamp: 1})
	buf.Push(SessionEvent{AgentID: "b", EventType: "ev", Timestamp: 2})
	buf.Clear("a")

	if buf.Len("b") != 1 {
		t.Errorf("Len('b') after clearing 'a' = %d, want 1", buf.Len("b"))
	}
}

func TestEventBufferLen(t *testing.T) {
	buf := NewEventBuffer(100)

	if buf.Len("a") != 0 {
		t.Errorf("Len for empty agent = %d, want 0", buf.Len("a"))
	}

	buf.Push(SessionEvent{AgentID: "a", EventType: "ev", Timestamp: 1})
	if buf.Len("a") != 1 {
		t.Errorf("Len after 1 push = %d, want 1", buf.Len("a"))
	}
}

func TestEventBufferDefaultSize(t *testing.T) {
	buf := NewEventBuffer(0)

	if buf.maxSize != DefaultEventBufSize {
		t.Errorf("maxSize = %d, want %d (default)", buf.maxSize, DefaultEventBufSize)
	}
}

func TestEventBufferDataPreserved(t *testing.T) {
	buf := NewEventBuffer(100)

	data := json.RawMessage(`{"key":"value","nested":{"n":42}}`)
	buf.Push(SessionEvent{
		AgentID:   "a",
		EventType: "test",
		SessionID: "sess-123",
		Timestamp: 1,
		Data:      data,
	})

	events := buf.Events("a")
	if len(events) != 1 {
		t.Fatalf("Events returned %d events, want 1", len(events))
	}
	if events[0].SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want %q", events[0].SessionID, "sess-123")
	}
	if string(events[0].Data) != string(data) {
		t.Errorf("Data = %s, want %s", events[0].Data, data)
	}
}

func TestEventBufferConcurrentPush(t *testing.T) {
	buf := NewEventBuffer(1000)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(agent string) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buf.Push(SessionEvent{
					AgentID:   agent,
					EventType: "concurrent",
					Timestamp: int64(j),
				})
			}
		}("agent-" + string(rune('a'+i)))
	}
	wg.Wait()

	// Each of the 10 agents should have exactly 100 events.
	for i := 0; i < 10; i++ {
		agent := "agent-" + string(rune('a'+i))
		if got := buf.Len(agent); got != 100 {
			t.Errorf("Len(%q) = %d, want 100", agent, got)
		}
	}
}
