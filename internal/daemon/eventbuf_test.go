package daemon

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestEventBufferPushAndEvents(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "session.created", Timestamp: 1})
	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "message.updated", Timestamp: 2})

	events := buf.Events("ses-1")
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

func TestEventBufferEventsReturnsNilForUnknownSession(t *testing.T) {
	buf := NewEventBuffer(100)

	events := buf.Events("nonexistent")
	if events != nil {
		t.Errorf("Events returned %v for unknown session, want nil", events)
	}
}

func TestEventBufferEventsReturnsCopy(t *testing.T) {
	buf := NewEventBuffer(100)
	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "original", Timestamp: 1})

	events := buf.Events("ses-1")
	events[0].EventType = "modified"

	original := buf.Events("ses-1")
	if original[0].EventType != "original" {
		t.Error("Events should return a copy, but modification affected the original")
	}
}

func TestEventBufferEvictsOldestWhenFull(t *testing.T) {
	buf := NewEventBuffer(3)

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-1", Timestamp: 1})
	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-2", Timestamp: 2})
	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-3", Timestamp: 3})
	// Buffer is now full. Next push should evict ev-1.
	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-4", Timestamp: 4})

	events := buf.Events("ses-1")
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

func TestEventBufferIsolatesSessions(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "s1-event", Timestamp: 1})
	buf.Push(SessionEvent{SessionID: "ses-2", EventType: "s2-event", Timestamp: 2})

	s1 := buf.Events("ses-1")
	s2 := buf.Events("ses-2")

	if len(s1) != 1 || s1[0].EventType != "s1-event" {
		t.Errorf("ses-1 events = %v, want [{EventType: s1-event}]", s1)
	}
	if len(s2) != 1 || s2[0].EventType != "s2-event" {
		t.Errorf("ses-2 events = %v, want [{EventType: s2-event}]", s2)
	}
}

func TestEventBufferEventsSince(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-1", Timestamp: 100})
	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-2", Timestamp: 200})
	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-3", Timestamp: 300})
	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-4", Timestamp: 400})

	// Events strictly after timestamp 200.
	events := buf.EventsSince("ses-1", 200)
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

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-1", Timestamp: 100})
	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-2", Timestamp: 200})

	// All events are after timestamp 0.
	events := buf.EventsSince("ses-1", 0)
	if len(events) != 2 {
		t.Fatalf("EventsSince(0) returned %d events, want 2", len(events))
	}
}

func TestEventBufferEventsSinceNoneAfter(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev-1", Timestamp: 100})

	events := buf.EventsSince("ses-1", 100)
	if events != nil {
		t.Errorf("EventsSince(100) returned %v, want nil (no events strictly after 100)", events)
	}
}

func TestEventBufferEventsSinceUnknownSession(t *testing.T) {
	buf := NewEventBuffer(100)

	events := buf.EventsSince("nonexistent", 0)
	if events != nil {
		t.Errorf("EventsSince for unknown session returned %v, want nil", events)
	}
}

func TestEventBufferClear(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev", Timestamp: 1})
	buf.Clear("ses-1")

	if buf.Len("ses-1") != 0 {
		t.Errorf("Len after Clear = %d, want 0", buf.Len("ses-1"))
	}
	if buf.Events("ses-1") != nil {
		t.Errorf("Events after Clear = %v, want nil", buf.Events("ses-1"))
	}
}

func TestEventBufferClearDoesNotAffectOtherSessions(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev", Timestamp: 1})
	buf.Push(SessionEvent{SessionID: "ses-2", EventType: "ev", Timestamp: 2})
	buf.Clear("ses-1")

	if buf.Len("ses-2") != 1 {
		t.Errorf("Len('ses-2') after clearing 'ses-1' = %d, want 1", buf.Len("ses-2"))
	}
}

func TestEventBufferLen(t *testing.T) {
	buf := NewEventBuffer(100)

	if buf.Len("ses-1") != 0 {
		t.Errorf("Len for empty session = %d, want 0", buf.Len("ses-1"))
	}

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev", Timestamp: 1})
	if buf.Len("ses-1") != 1 {
		t.Errorf("Len after 1 push = %d, want 1", buf.Len("ses-1"))
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
		EventType: "test",
		SessionID: "ses-123",
		Timestamp: 1,
		Data:      data,
	})

	events := buf.Events("ses-123")
	if len(events) != 1 {
		t.Fatalf("Events returned %d events, want 1", len(events))
	}
	if events[0].SessionID != "ses-123" {
		t.Errorf("SessionID = %q, want %q", events[0].SessionID, "ses-123")
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
		go func(session string) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buf.Push(SessionEvent{
					SessionID: session,
					EventType: "concurrent",
					Timestamp: int64(j),
				})
			}
		}("ses-" + string(rune('a'+i)))
	}
	wg.Wait()

	// Each of the 10 sessions should have exactly 100 events.
	for i := 0; i < 10; i++ {
		session := "ses-" + string(rune('a'+i))
		if got := buf.Len(session); got != 100 {
			t.Errorf("Len(%q) = %d, want 100", session, got)
		}
	}
}

func TestEventBufferSweepIdleRemovesStale(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev", Timestamp: 1})
	buf.Push(SessionEvent{SessionID: "ses-2", EventType: "ev", Timestamp: 2})

	if buf.SessionCount() != 2 {
		t.Fatalf("SessionCount = %d, want 2", buf.SessionCount())
	}

	// Manually set lastPush to the past to simulate idle sessions.
	buf.mu.Lock()
	buf.sessions["ses-1"].lastPush = time.Now().Add(-2 * sessionIdleTTL)
	buf.mu.Unlock()

	removed := buf.SweepIdle()
	if removed != 1 {
		t.Errorf("SweepIdle removed %d sessions, want 1", removed)
	}
	if buf.SessionCount() != 1 {
		t.Errorf("SessionCount after sweep = %d, want 1", buf.SessionCount())
	}
	if buf.Len("ses-1") != 0 {
		t.Errorf("ses-1 should be gone after sweep, but has %d events", buf.Len("ses-1"))
	}
	if buf.Len("ses-2") != 1 {
		t.Errorf("ses-2 should still exist, but has %d events", buf.Len("ses-2"))
	}
}

func TestEventBufferSweepIdleKeepsActive(t *testing.T) {
	buf := NewEventBuffer(100)

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev", Timestamp: 1})

	// Recent push — should not be swept.
	removed := buf.SweepIdle()
	if removed != 0 {
		t.Errorf("SweepIdle removed %d sessions, want 0 (all active)", removed)
	}
	if buf.SessionCount() != 1 {
		t.Errorf("SessionCount = %d, want 1", buf.SessionCount())
	}
}

func TestEventBufferSessionCount(t *testing.T) {
	buf := NewEventBuffer(100)

	if buf.SessionCount() != 0 {
		t.Errorf("SessionCount for empty buffer = %d, want 0", buf.SessionCount())
	}

	buf.Push(SessionEvent{SessionID: "ses-1", EventType: "ev", Timestamp: 1})
	buf.Push(SessionEvent{SessionID: "ses-2", EventType: "ev", Timestamp: 2})

	if buf.SessionCount() != 2 {
		t.Errorf("SessionCount = %d, want 2", buf.SessionCount())
	}

	buf.Clear("ses-1")
	if buf.SessionCount() != 1 {
		t.Errorf("SessionCount after Clear = %d, want 1", buf.SessionCount())
	}
}
