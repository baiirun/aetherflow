package daemon

import (
	"strings"
	"testing"
)

// --- FormatEvent tests ---

func TestFormatEvent_Tool(t *testing.T) {
	ev := SessionEvent{
		EventType: "message.part.updated",
		SessionID: "ses-1",
		Timestamp: 1770534051455,
		Data:      []byte(`{"part":{"type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"cargo build","description":"Build project"},"title":"Build project","time":{"start":1770534051422,"end":1770534053453}}}}`),
	}

	result := FormatEvent(ev)
	if result == "" {
		t.Fatal("expected non-empty result for tool event")
	}
	if !strings.Contains(result, "bash") {
		t.Errorf("result should contain tool name, got: %s", result)
	}
	if !strings.Contains(result, "Build project") {
		t.Errorf("result should contain title, got: %s", result)
	}
	if !strings.Contains(result, "✓") {
		t.Errorf("result should contain success icon, got: %s", result)
	}
}

func TestFormatEvent_Text(t *testing.T) {
	ev := SessionEvent{
		EventType: "message.part.updated",
		SessionID: "ses-1",
		Timestamp: 1770534050893,
		Data:      []byte(`{"part":{"type":"text","text":"Starting implementation now."}}`),
	}

	result := FormatEvent(ev)
	if result == "" {
		t.Fatal("expected non-empty result for text event")
	}
	if !strings.Contains(result, "Starting implementation now.") {
		t.Errorf("result should contain text, got: %s", result)
	}
}

func TestFormatEvent_StepFinish(t *testing.T) {
	ev := SessionEvent{
		EventType: "message.part.updated",
		SessionID: "ses-1",
		Timestamp: 1770532976651,
		Data:      []byte(`{"part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":0,"output":126,"reasoning":0,"cache":{"read":101522,"write":273}}}}`),
	}

	result := FormatEvent(ev)
	if result == "" {
		t.Fatal("expected non-empty result for step-finish event")
	}
	if !strings.Contains(result, "step") {
		t.Errorf("result should contain step marker, got: %s", result)
	}
	if !strings.Contains(result, "tool-calls") {
		t.Errorf("result should contain reason, got: %s", result)
	}
}

func TestFormatEvent_StepStart(t *testing.T) {
	ev := SessionEvent{
		EventType: "message.part.updated",
		SessionID: "ses-1",
		Timestamp: 1770534050401,
		Data:      []byte(`{"part":{"type":"step-start"}}`),
	}

	result := FormatEvent(ev)
	if result != "" {
		t.Errorf("step-start should be hidden, got: %s", result)
	}
}

func TestFormatEvent_NonPartEvent(t *testing.T) {
	ev := SessionEvent{
		EventType: "session.created",
		SessionID: "ses-1",
		Timestamp: 1000,
		Data:      []byte(`{"info":{"id":"ses-1"}}`),
	}

	result := FormatEvent(ev)
	if result != "" {
		t.Errorf("non-part events should be hidden, got: %s", result)
	}
}

func TestFormatEvent_EmptyData(t *testing.T) {
	ev := SessionEvent{
		EventType: "message.part.updated",
		SessionID: "ses-1",
		Timestamp: 1000,
	}

	result := FormatEvent(ev)
	if result != "" {
		t.Errorf("empty data should return empty, got: %s", result)
	}
}

func TestFormatEvent_InvalidJSON(t *testing.T) {
	ev := SessionEvent{
		EventType: "message.part.updated",
		SessionID: "ses-1",
		Timestamp: 1000,
		Data:      []byte(`not json`),
	}

	result := FormatEvent(ev)
	if result != "" {
		t.Errorf("invalid JSON should return empty, got: %s", result)
	}
}

func TestFormatMs(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{50, "50ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{2031, "2.0s"},
		{59999, "60.0s"},
		{60000, "1m0s"},
		{90000, "1m30s"},
	}
	for _, tt := range tests {
		got := formatMs(tt.ms)
		if got != tt.want {
			t.Errorf("formatMs(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestTruncateStr(t *testing.T) {
	if got := truncateStr("hello", 10); got != "hello" {
		t.Errorf("short string should not truncate: %q", got)
	}
	if got := truncateStr("hello world this is long", 10); got != "hello wor…" {
		t.Errorf("long string should truncate: %q", got)
	}
}
