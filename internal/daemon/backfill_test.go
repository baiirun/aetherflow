package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/baiirun/aetherflow/internal/sessions"
)

// newTestOpencodeServer creates a mock opencode REST API that serves
// the given messages for each session. Returns the server and a cleanup function.
func newTestOpencodeServer(t *testing.T, sessionMessages map[string][]apiMessage) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// GET /session/:id/message
	mux.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		// Parse path: /session/<id>/message
		var sessionID string
		n, _ := fmt.Sscanf(r.URL.Path, "/session/%s", &sessionID)
		if n != 1 {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		// Strip trailing "/message" from sessionID
		if len(sessionID) > 8 && sessionID[len(sessionID)-8:] == "/message" {
			sessionID = sessionID[:len(sessionID)-8]
		}

		msgs, ok := sessionMessages[sessionID]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msgs)
	})

	return httptest.NewServer(mux)
}

func TestBackfillSessionConvertsParts(t *testing.T) {
	// Tool part matching the shape from opencode's SQLite/REST API.
	toolPart := json.RawMessage(`{
		"id": "prt_tool1",
		"sessionID": "ses_test",
		"messageID": "msg_1",
		"type": "tool",
		"tool": "bash",
		"state": {
			"status": "completed",
			"input": {"command": "echo hello"},
			"title": "Run echo",
			"time": {"start": 1700000000000, "end": 1700000000500}
		}
	}`)

	textPart := json.RawMessage(`{
		"id": "prt_text1",
		"sessionID": "ses_test",
		"messageID": "msg_1",
		"type": "text",
		"text": "Hello world"
	}`)

	server := newTestOpencodeServer(t, map[string][]apiMessage{
		"ses_test": {
			{
				ID:    "msg_1",
				Parts: []json.RawMessage{textPart, toolPart},
			},
		},
	})
	defer server.Close()

	api := newOpencodeClient(server.URL)
	events := NewEventBuffer(DefaultEventBufSize)

	n, err := backfillSession(context.Background(), api, events, "ses_test")
	if err != nil {
		t.Fatalf("backfillSession error: %v", err)
	}
	if n != 2 {
		t.Errorf("backfillSession returned %d events, want 2", n)
	}

	// Verify events are in the buffer.
	allEvents := events.Events("ses_test")
	if len(allEvents) != 2 {
		t.Fatalf("buffer has %d events, want 2", len(allEvents))
	}

	// Both should be message.part.updated.
	for i, ev := range allEvents {
		if ev.EventType != "message.part.updated" {
			t.Errorf("event[%d].EventType = %q, want %q", i, ev.EventType, "message.part.updated")
		}
		if ev.SessionID != "ses_test" {
			t.Errorf("event[%d].SessionID = %q, want %q", i, ev.SessionID, "ses_test")
		}
	}

	// The tool event should have a non-zero timestamp from state.time.start.
	if allEvents[1].Timestamp != 1700000000000 {
		t.Errorf("tool event timestamp = %d, want 1700000000000", allEvents[1].Timestamp)
	}

	// The text event has no time, so timestamp should be 0.
	if allEvents[0].Timestamp != 0 {
		t.Errorf("text event timestamp = %d, want 0", allEvents[0].Timestamp)
	}

	// Verify the event data has the {"part": ...} envelope.
	var envelope struct {
		Part json.RawMessage `json:"part"`
	}
	if err := json.Unmarshal(allEvents[1].Data, &envelope); err != nil {
		t.Fatalf("failed to parse event data: %v", err)
	}
	if len(envelope.Part) == 0 {
		t.Error("event data should have 'part' field wrapping the raw part")
	}

	// Verify ToolCallsFromEvents can extract the tool call from backfilled events.
	calls := ToolCallsFromEvents(allEvents, 0)
	if len(calls) != 1 {
		t.Fatalf("ToolCallsFromEvents returned %d calls, want 1", len(calls))
	}
	if calls[0].Tool != "bash" {
		t.Errorf("tool = %q, want %q", calls[0].Tool, "bash")
	}
	if calls[0].Status != "completed" {
		t.Errorf("status = %q, want %q", calls[0].Status, "completed")
	}
	if calls[0].DurationMs != 500 {
		t.Errorf("duration = %d, want 500", calls[0].DurationMs)
	}
}

func TestBackfillEventsSkipsExistingSessions(t *testing.T) {
	toolPart := json.RawMessage(`{
		"id": "prt_1",
		"type": "tool",
		"tool": "bash",
		"state": {"status": "completed", "input": {"command": "ls"}}
	}`)

	server := newTestOpencodeServer(t, map[string][]apiMessage{
		"ses_existing": {{ID: "msg_1", Parts: []json.RawMessage{toolPart}}},
		"ses_empty":    {{ID: "msg_2", Parts: []json.RawMessage{toolPart}}},
	})
	defer server.Close()

	api := newOpencodeClient(server.URL)
	events := NewEventBuffer(DefaultEventBufSize)

	// Pre-populate events for ses_existing — backfill should skip it.
	events.Push(SessionEvent{
		EventType: "message.part.updated",
		SessionID: "ses_existing",
		Timestamp: 1700000000000,
		Data:      json.RawMessage(`{"part": {"type": "text"}}`),
	})

	// Create a session store with two records.
	dir := t.TempDir()
	store, err := sessions.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Upsert(sessions.Record{
		ServerRef: "http://127.0.0.1:4096",
		SessionID: "ses_existing",
		Status:    sessions.StatusActive,
	})
	_ = store.Upsert(sessions.Record{
		ServerRef: "http://127.0.0.1:4096",
		SessionID: "ses_empty",
		Status:    sessions.StatusActive,
	})

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	backfillEvents(context.Background(), api, store, events, log)

	// ses_existing should still have exactly 1 event (the pre-populated one).
	if events.Len("ses_existing") != 1 {
		t.Errorf("ses_existing has %d events, want 1 (should be skipped)", events.Len("ses_existing"))
	}

	// ses_empty should have been backfilled with 1 event.
	if events.Len("ses_empty") != 1 {
		t.Errorf("ses_empty has %d events, want 1 (should be backfilled)", events.Len("ses_empty"))
	}
}

func TestBackfillEventsSkipsTerminatedSessions(t *testing.T) {
	toolPart := json.RawMessage(`{"id": "prt_1", "type": "text", "text": "hello"}`)

	server := newTestOpencodeServer(t, map[string][]apiMessage{
		"ses_terminated": {{ID: "msg_1", Parts: []json.RawMessage{toolPart}}},
		"ses_active":     {{ID: "msg_2", Parts: []json.RawMessage{toolPart}}},
	})
	defer server.Close()

	api := newOpencodeClient(server.URL)
	events := NewEventBuffer(DefaultEventBufSize)

	dir := t.TempDir()
	store, err := sessions.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Upsert(sessions.Record{
		ServerRef: "http://127.0.0.1:4096",
		SessionID: "ses_terminated",
		Status:    sessions.StatusTerminated,
	})
	_ = store.Upsert(sessions.Record{
		ServerRef: "http://127.0.0.1:4096",
		SessionID: "ses_active",
		Status:    sessions.StatusActive,
	})

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	backfillEvents(context.Background(), api, store, events, log)

	// Terminated session should not be backfilled.
	if events.Len("ses_terminated") != 0 {
		t.Errorf("ses_terminated has %d events, want 0", events.Len("ses_terminated"))
	}

	// Active session should be backfilled.
	if events.Len("ses_active") != 1 {
		t.Errorf("ses_active has %d events, want 1", events.Len("ses_active"))
	}
}

func TestBackfillSessionAPIError(t *testing.T) {
	// Server returns 500 for all sessions.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	api := newOpencodeClient(server.URL)
	events := NewEventBuffer(DefaultEventBufSize)

	_, err := backfillSession(context.Background(), api, events, "ses_fail")
	if err == nil {
		t.Error("expected error from backfillSession when API returns 500")
	}

	// Buffer should be empty.
	if events.Len("ses_fail") != 0 {
		t.Errorf("buffer has %d events after error, want 0", events.Len("ses_fail"))
	}
}

func TestBackfillEventsNilStore(t *testing.T) {
	// Should not panic with nil store.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	backfillEvents(context.Background(), newOpencodeClient("http://localhost"), nil, NewEventBuffer(100), log)
}

func TestPartToEventEnvelope(t *testing.T) {
	rawPart := json.RawMessage(`{
		"id": "prt_abc",
		"type": "tool",
		"tool": "bash",
		"state": {
			"status": "completed",
			"input": {"command": "echo test"},
			"time": {"start": 1700000001000, "end": 1700000002000}
		}
	}`)

	ev, err := partToEvent("ses_xyz", rawPart)
	if err != nil {
		t.Fatalf("partToEvent error: %v", err)
	}

	if ev.EventType != "message.part.updated" {
		t.Errorf("EventType = %q, want %q", ev.EventType, "message.part.updated")
	}
	if ev.SessionID != "ses_xyz" {
		t.Errorf("SessionID = %q, want %q", ev.SessionID, "ses_xyz")
	}
	if ev.Timestamp != 1700000001000 {
		t.Errorf("Timestamp = %d, want 1700000001000", ev.Timestamp)
	}

	// Verify the data envelope wraps the part correctly.
	var envelope struct {
		Part json.RawMessage `json:"part"`
	}
	if err := json.Unmarshal(ev.Data, &envelope); err != nil {
		t.Fatalf("failed to parse Data: %v", err)
	}

	var part struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(envelope.Part, &part); err != nil {
		t.Fatalf("failed to parse part: %v", err)
	}
	if part.ID != "prt_abc" {
		t.Errorf("part.id = %q, want %q", part.ID, "prt_abc")
	}
	if part.Type != "tool" {
		t.Errorf("part.type = %q, want %q", part.Type, "tool")
	}
}

func TestExtractPartTimestamp(t *testing.T) {
	tests := []struct {
		name string
		json string
		want int64
	}{
		{
			name: "tool part with state.time.start",
			json: `{"type":"tool","state":{"time":{"start":1700000000000,"end":1700000001000}}}`,
			want: 1700000000000,
		},
		{
			name: "reasoning part with time.start",
			json: `{"type":"reasoning","time":{"start":1700000002000,"end":1700000003000}}`,
			want: 1700000002000,
		},
		{
			name: "text part with no time",
			json: `{"type":"text","text":"hello"}`,
			want: 0,
		},
		{
			name: "empty object",
			json: `{}`,
			want: 0,
		},
		{
			name: "both state.time and time — state.time wins",
			json: `{"state":{"time":{"start":100}},"time":{"start":200}}`,
			want: 100,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractPartTimestamp(json.RawMessage(tc.json))
			if got != tc.want {
				t.Errorf("extractPartTimestamp() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBackfillMultipleMessages(t *testing.T) {
	part1 := json.RawMessage(`{"id": "prt_1", "type": "text", "text": "prompt"}`)
	part2 := json.RawMessage(`{
		"id": "prt_2",
		"type": "tool",
		"tool": "bash",
		"state": {"status": "completed", "input": {"command": "echo 1"}, "time": {"start": 1700000000000, "end": 1700000000100}}
	}`)
	part3 := json.RawMessage(`{
		"id": "prt_3",
		"type": "tool",
		"tool": "bash",
		"state": {"status": "completed", "input": {"command": "echo 2"}, "time": {"start": 1700000000200, "end": 1700000000300}}
	}`)

	server := newTestOpencodeServer(t, map[string][]apiMessage{
		"ses_multi": {
			{ID: "msg_1", Parts: []json.RawMessage{part1}},
			{ID: "msg_2", Parts: []json.RawMessage{part2, part3}},
		},
	})
	defer server.Close()

	api := newOpencodeClient(server.URL)
	events := NewEventBuffer(DefaultEventBufSize)

	n, err := backfillSession(context.Background(), api, events, "ses_multi")
	if err != nil {
		t.Fatalf("backfillSession error: %v", err)
	}
	if n != 3 {
		t.Errorf("backfillSession returned %d events, want 3", n)
	}

	// Verify ToolCallsFromEvents finds both tool calls.
	allEvents := events.Events("ses_multi")
	calls := ToolCallsFromEvents(allEvents, 0)
	if len(calls) != 2 {
		t.Fatalf("ToolCallsFromEvents returned %d calls, want 2", len(calls))
	}
}

func TestFetchSessionMessagesOK(t *testing.T) {
	part := json.RawMessage(`{"id": "prt_1", "type": "text", "text": "hi"}`)
	server := newTestOpencodeServer(t, map[string][]apiMessage{
		"ses_ok": {{ID: "msg_1", Parts: []json.RawMessage{part}}},
	})
	defer server.Close()

	api := newOpencodeClient(server.URL)
	msgs, err := api.fetchSessionMessages(context.Background(), "ses_ok")
	if err != nil {
		t.Fatalf("fetchSessionMessages error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].ID != "msg_1" {
		t.Errorf("message ID = %q, want %q", msgs[0].ID, "msg_1")
	}
	if len(msgs[0].Parts) != 1 {
		t.Errorf("message has %d parts, want 1", len(msgs[0].Parts))
	}
}

func TestFetchSessionMessages404(t *testing.T) {
	server := newTestOpencodeServer(t, map[string][]apiMessage{})
	defer server.Close()

	api := newOpencodeClient(server.URL)
	_, err := api.fetchSessionMessages(context.Background(), "ses_missing")
	if err == nil {
		t.Error("expected error for 404, got nil")
	}
}

func TestFetchSessionMessagesTimeout(t *testing.T) {
	// Server that never responds.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	api := &opencodeClient{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 100 * time.Millisecond},
	}

	ctx := context.Background()
	_, err := api.fetchSessionMessages(ctx, "ses_slow")
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}
