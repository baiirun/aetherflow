package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
)

func newTestDaemonForEvents() *Daemon {
	return &Daemon{
		events: NewEventBuffer(DefaultEventBufSize),
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

func TestHandleSessionEventSuccess(t *testing.T) {
	d := newTestDaemonForEvents()

	params, _ := json.Marshal(SessionEventParams{
		AgentID:   "agent-1",
		EventType: "session.created",
		SessionID: "sess-abc",
		Timestamp: 1000,
		Data:      json.RawMessage(`{"info":{"id":"sess-abc"}}`),
	})

	resp := d.handleSessionEvent(params)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	events := d.events.Events("agent-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event in buffer, got %d", len(events))
	}
	ev := events[0]
	if ev.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", ev.AgentID, "agent-1")
	}
	if ev.EventType != "session.created" {
		t.Errorf("EventType = %q, want %q", ev.EventType, "session.created")
	}
	if ev.SessionID != "sess-abc" {
		t.Errorf("SessionID = %q, want %q", ev.SessionID, "sess-abc")
	}
	if ev.Timestamp != 1000 {
		t.Errorf("Timestamp = %d, want 1000", ev.Timestamp)
	}
	if string(ev.Data) != `{"info":{"id":"sess-abc"}}` {
		t.Errorf("Data = %s, want %s", ev.Data, `{"info":{"id":"sess-abc"}}`)
	}
}

func TestHandleSessionEventMissingAgentID(t *testing.T) {
	d := newTestDaemonForEvents()

	params, _ := json.Marshal(SessionEventParams{
		EventType: "session.created",
		Timestamp: 1000,
	})

	resp := d.handleSessionEvent(params)
	if resp.Success {
		t.Fatal("expected failure for missing agent_id")
	}
	if resp.Error != "agent_id is required" {
		t.Errorf("Error = %q, want %q", resp.Error, "agent_id is required")
	}
}

func TestHandleSessionEventMissingEventType(t *testing.T) {
	d := newTestDaemonForEvents()

	params, _ := json.Marshal(SessionEventParams{
		AgentID:   "agent-1",
		Timestamp: 1000,
	})

	resp := d.handleSessionEvent(params)
	if resp.Success {
		t.Fatal("expected failure for missing event_type")
	}
	if resp.Error != "event_type is required" {
		t.Errorf("Error = %q, want %q", resp.Error, "event_type is required")
	}
}

func TestHandleSessionEventInvalidJSON(t *testing.T) {
	d := newTestDaemonForEvents()

	resp := d.handleSessionEvent(json.RawMessage(`{invalid json`))
	if resp.Success {
		t.Fatal("expected failure for invalid JSON")
	}
}

func TestHandleSessionEventOptionalSessionID(t *testing.T) {
	d := newTestDaemonForEvents()

	params, _ := json.Marshal(SessionEventParams{
		AgentID:   "agent-1",
		EventType: "session.status",
		Timestamp: 1000,
	})

	resp := d.handleSessionEvent(params)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	events := d.events.Events("agent-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].SessionID != "" {
		t.Errorf("SessionID = %q, want empty", events[0].SessionID)
	}
}

func TestHandleSessionEventMultipleEvents(t *testing.T) {
	d := newTestDaemonForEvents()

	types := []string{"session.created", "message.updated", "message.part.updated", "session.idle"}
	for i, et := range types {
		params, _ := json.Marshal(SessionEventParams{
			AgentID:   "agent-1",
			EventType: et,
			Timestamp: int64(i + 1),
		})
		resp := d.handleSessionEvent(params)
		if !resp.Success {
			t.Fatalf("event %d (%s) failed: %s", i, et, resp.Error)
		}
	}

	if d.events.Len("agent-1") != 4 {
		t.Errorf("Len = %d, want 4", d.events.Len("agent-1"))
	}

	events := d.events.Events("agent-1")
	for i, et := range types {
		if events[i].EventType != et {
			t.Errorf("events[%d].EventType = %q, want %q", i, events[i].EventType, et)
		}
	}
}

func TestHandleSessionEventIsolatesAgents(t *testing.T) {
	d := newTestDaemonForEvents()

	for _, agent := range []string{"agent-1", "agent-2"} {
		params, _ := json.Marshal(SessionEventParams{
			AgentID:   agent,
			EventType: "session.created",
			Timestamp: 1,
		})
		resp := d.handleSessionEvent(params)
		if !resp.Success {
			t.Fatalf("event for %s failed: %s", agent, resp.Error)
		}
	}

	if d.events.Len("agent-1") != 1 {
		t.Errorf("agent-1 Len = %d, want 1", d.events.Len("agent-1"))
	}
	if d.events.Len("agent-2") != 1 {
		t.Errorf("agent-2 Len = %d, want 1", d.events.Len("agent-2"))
	}
}
