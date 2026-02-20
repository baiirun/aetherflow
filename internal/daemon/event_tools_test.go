package daemon

import (
	"encoding/json"
	"testing"
)

func TestToolCallsFromEventsHappyPath(t *testing.T) {
	events := []SessionEvent{
		{EventType: "session.created", SessionID: "ses-1", Timestamp: 1000},
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 2000,
			Data: json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/project/auth.go"},"title":"auth.go","time":{"start":1000,"end":2000}}}}`)},
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 3000,
			Data: json.RawMessage(`{"part":{"id":"prt_2","type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"go test ./..."},"time":{"start":2500,"end":3000}}}}`)},
		{EventType: "session.idle", SessionID: "ses-1", Timestamp: 4000},
	}

	calls := ToolCallsFromEvents(events, 0)

	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}

	if calls[0].Tool != "read" {
		t.Errorf("call[0].Tool = %q, want %q", calls[0].Tool, "read")
	}
	if calls[0].Input != "/project/auth.go" {
		t.Errorf("call[0].Input = %q, want %q", calls[0].Input, "/project/auth.go")
	}
	if calls[0].Title != "auth.go" {
		t.Errorf("call[0].Title = %q, want %q", calls[0].Title, "auth.go")
	}
	if calls[0].Status != "completed" {
		t.Errorf("call[0].Status = %q, want %q", calls[0].Status, "completed")
	}
	if calls[0].DurationMs != 1000 {
		t.Errorf("call[0].DurationMs = %d, want 1000", calls[0].DurationMs)
	}

	if calls[1].Tool != "bash" {
		t.Errorf("call[1].Tool = %q, want %q", calls[1].Tool, "bash")
	}
	if calls[1].Input != "go test ./..." {
		t.Errorf("call[1].Input = %q, want %q", calls[1].Input, "go test ./...")
	}
	if calls[1].DurationMs != 500 {
		t.Errorf("call[1].DurationMs = %d, want 500", calls[1].DurationMs)
	}
}

func TestToolCallsFromEventsDeduplicatesByPartID(t *testing.T) {
	// Same part ID with lifecycle: pending → running → completed.
	// Should produce only one tool call with the final (completed) state.
	events := []SessionEvent{
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 1000,
			Data: json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"bash","state":{"status":"pending","input":{}}}}`)},
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 2000,
			Data: json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"bash","state":{"status":"running","input":{"command":"echo hello"},"time":{"start":2000}}}}`)},
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 3000,
			Data: json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"echo hello"},"title":"Echo hello","time":{"start":2000,"end":3000}}}}`)},
	}

	calls := ToolCallsFromEvents(events, 0)

	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1 (deduplicated)", len(calls))
	}
	if calls[0].Status != "completed" {
		t.Errorf("Status = %q, want %q", calls[0].Status, "completed")
	}
	if calls[0].Title != "Echo hello" {
		t.Errorf("Title = %q, want %q", calls[0].Title, "Echo hello")
	}
	if calls[0].DurationMs != 1000 {
		t.Errorf("DurationMs = %d, want 1000", calls[0].DurationMs)
	}
}

func TestToolCallsFromEventsLimit(t *testing.T) {
	events := []SessionEvent{
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 1000,
			Data: json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/a"}}}}`)},
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 2000,
			Data: json.RawMessage(`{"part":{"id":"prt_2","type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/b"}}}}`)},
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 3000,
			Data: json.RawMessage(`{"part":{"id":"prt_3","type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/c"}}}}`)},
	}

	calls := ToolCallsFromEvents(events, 2)

	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2 (limited)", len(calls))
	}
	// Should return the last 2 (most recent).
	if calls[0].Input != "/b" {
		t.Errorf("call[0].Input = %q, want %q", calls[0].Input, "/b")
	}
	if calls[1].Input != "/c" {
		t.Errorf("call[1].Input = %q, want %q", calls[1].Input, "/c")
	}
}

func TestToolCallsFromEventsEmptyEvents(t *testing.T) {
	calls := ToolCallsFromEvents(nil, 0)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for nil events, got %d", len(calls))
	}

	calls = ToolCallsFromEvents([]SessionEvent{}, 0)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for empty events, got %d", len(calls))
	}
}

func TestToolCallsFromEventsSkipsNonToolParts(t *testing.T) {
	events := []SessionEvent{
		// text part — should be skipped
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 1000,
			Data: json.RawMessage(`{"part":{"id":"prt_1","type":"text","text":"Hello world"}}`)},
		// step-start — should be skipped
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 2000,
			Data: json.RawMessage(`{"part":{"id":"prt_2","type":"step-start","snapshot":"abc123"}}`)},
		// tool — should be included
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 3000,
			Data: json.RawMessage(`{"part":{"id":"prt_3","type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"ls"}}}}`)},
		// step-finish — should be skipped
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 4000,
			Data: json.RawMessage(`{"part":{"id":"prt_4","type":"step-finish","reason":"tool-calls"}}`)},
	}

	calls := ToolCallsFromEvents(events, 0)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1 (only tool parts)", len(calls))
	}
	if calls[0].Tool != "bash" {
		t.Errorf("Tool = %q, want %q", calls[0].Tool, "bash")
	}
}

func TestToolCallsFromEventsSkipsNonPartEvents(t *testing.T) {
	events := []SessionEvent{
		{EventType: "session.created", SessionID: "ses-1", Timestamp: 1000},
		{EventType: "session.status", SessionID: "ses-1", Timestamp: 2000},
		{EventType: "message.updated", SessionID: "ses-1", Timestamp: 3000},
		{EventType: "session.idle", SessionID: "ses-1", Timestamp: 4000},
	}

	calls := ToolCallsFromEvents(events, 0)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for non-part events, got %d", len(calls))
	}
}

func TestToolCallsFromEventsHandlesMalformedData(t *testing.T) {
	events := []SessionEvent{
		// Malformed JSON data
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 1000,
			Data: json.RawMessage(`{invalid json}`)},
		// Empty data
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 2000,
			Data: nil},
		// Valid tool call
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 3000,
			Data: json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"ls"}}}}`)},
	}

	calls := ToolCallsFromEvents(events, 0)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1 (skipping malformed)", len(calls))
	}
	if calls[0].Tool != "bash" {
		t.Errorf("Tool = %q, want %q", calls[0].Tool, "bash")
	}
}

func TestToolCallsFromEventsPreservesOrder(t *testing.T) {
	// Multiple different tool calls — should preserve chronological order.
	events := []SessionEvent{
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 1000,
			Data: json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/a"}}}}`)},
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 2000,
			Data: json.RawMessage(`{"part":{"id":"prt_2","type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"go build"}}}}`)},
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 3000,
			Data: json.RawMessage(`{"part":{"id":"prt_3","type":"tool","tool":"edit","state":{"status":"completed","input":{"filePath":"/b"}}}}`)},
	}

	calls := ToolCallsFromEvents(events, 0)
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}
	if calls[0].Tool != "read" {
		t.Errorf("call[0].Tool = %q, want %q", calls[0].Tool, "read")
	}
	if calls[1].Tool != "bash" {
		t.Errorf("call[1].Tool = %q, want %q", calls[1].Tool, "bash")
	}
	if calls[2].Tool != "edit" {
		t.Errorf("call[2].Tool = %q, want %q", calls[2].Tool, "edit")
	}
}

func TestExtractKeyInput(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{"read filePath", "read", `{"filePath":"/foo/bar.go"}`, "/foo/bar.go"},
		{"edit filePath", "edit", `{"filePath":"/foo/bar.go","oldString":"a","newString":"b"}`, "/foo/bar.go"},
		{"write filePath", "write", `{"filePath":"/foo/bar.go","content":"x"}`, "/foo/bar.go"},
		{"bash command", "bash", `{"command":"go test ./..."}`, "go test ./..."},
		{"glob pattern", "glob", `{"pattern":"**/*.go"}`, "**/*.go"},
		{"grep pattern", "grep", `{"pattern":"func main"}`, "func main"},
		{"task description", "task", `{"description":"review code"}`, "review code"},
		{"skill name", "skill", `{"name":"review-auto"}`, "review-auto"},
		{"unknown with url", "webfetch", `{"url":"https://example.com"}`, "https://example.com"},
		{"unknown no known fields", "custom", `{"foo":"bar"}`, ""},
		{"empty input", "read", `{}`, ""},
		{"null input", "read", ``, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractKeyInput(tt.tool, []byte(tt.input))
			if got != tt.want {
				t.Errorf("extractKeyInput(%q, %q) = %q, want %q", tt.tool, tt.input, got, tt.want)
			}
		})
	}
}

func TestToolCallsFromEventsNoDuration(t *testing.T) {
	events := []SessionEvent{
		{EventType: "message.part.updated", SessionID: "ses-1", Timestamp: 1000,
			Data: json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/foo"}}}}`)},
	}

	calls := ToolCallsFromEvents(events, 0)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].DurationMs != 0 {
		t.Errorf("DurationMs = %d, want 0 (no time data)", calls[0].DurationMs)
	}
}
