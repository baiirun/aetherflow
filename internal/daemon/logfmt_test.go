package daemon

import (
	"strings"
	"testing"
)

func TestFormatLogLine_Text(t *testing.T) {
	raw := `{"type":"text","timestamp":1770534050893,"part":{"type":"text","text":"Starting implementation now.","time":{"start":1770534050890,"end":1770534050890}}}`

	result := FormatLogLine([]byte(raw))
	if result == "" {
		t.Fatal("expected non-empty result for text event")
	}
	if !strings.Contains(result, "Starting implementation now.") {
		t.Errorf("result should contain text, got: %s", result)
	}
}

func TestFormatLogLine_ToolUse(t *testing.T) {
	raw := `{"type":"tool_use","timestamp":1770534051455,"part":{"tool":"bash","state":{"status":"completed","input":{"command":"cargo build","description":"Build project"},"title":"Build project","time":{"start":1770534051422,"end":1770534053453}}}}`

	result := FormatLogLine([]byte(raw))
	if result == "" {
		t.Fatal("expected non-empty result for tool_use event")
	}
	if !strings.Contains(result, "bash") {
		t.Errorf("result should contain tool name, got: %s", result)
	}
	if !strings.Contains(result, "Build project") {
		t.Errorf("result should contain title, got: %s", result)
	}
	if !strings.Contains(result, "2.0s") {
		t.Errorf("result should contain duration, got: %s", result)
	}
	if !strings.Contains(result, "✓") {
		t.Errorf("result should contain success icon, got: %s", result)
	}
}

func TestFormatLogLine_ToolUseError(t *testing.T) {
	raw := `{"type":"tool_use","timestamp":1770534051455,"part":{"tool":"bash","state":{"status":"error","input":{"command":"cargo test"},"title":"Run tests","time":{"start":1770534051422,"end":1770534053453}}}}`

	result := FormatLogLine([]byte(raw))
	if !strings.Contains(result, "✗") {
		t.Errorf("result should contain error icon, got: %s", result)
	}
}

func TestFormatLogLine_StepFinish(t *testing.T) {
	raw := `{"type":"step_finish","timestamp":1770532976651,"part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":0,"output":126,"reasoning":0,"cache":{"read":101522,"write":273}}}}`

	result := FormatLogLine([]byte(raw))
	if result == "" {
		t.Fatal("expected non-empty result for step_finish event")
	}
	if !strings.Contains(result, "step") {
		t.Errorf("result should contain step marker, got: %s", result)
	}
	if !strings.Contains(result, "tool-calls") {
		t.Errorf("result should contain reason, got: %s", result)
	}
}

func TestFormatLogLine_StepStart(t *testing.T) {
	raw := `{"type":"step_start","timestamp":1770534050401,"part":{"type":"step-start"}}`

	result := FormatLogLine([]byte(raw))
	if result != "" {
		t.Errorf("step_start should be hidden, got: %s", result)
	}
}

func TestFormatLogLine_InvalidJSON(t *testing.T) {
	result := FormatLogLine([]byte("not json"))
	if result != "" {
		t.Errorf("invalid JSON should return empty, got: %s", result)
	}
}

func TestFormatLogLine_SkillTool(t *testing.T) {
	raw := `{"type":"tool_use","timestamp":1770532979544,"part":{"tool":"skill","state":{"status":"completed","input":{"name":"review-auto"},"title":"Loaded skill: review-auto","time":{"start":1770532979512,"end":1770532979544}}}}`

	result := FormatLogLine([]byte(raw))
	if !strings.Contains(result, "skill") {
		t.Errorf("result should contain tool name, got: %s", result)
	}
	if !strings.Contains(result, "review-auto") {
		t.Errorf("result should contain skill name, got: %s", result)
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

func TestFormatLogLine_ClaudeToolUse(t *testing.T) {
	result := FormatLogLine([]byte(claudeAssistantToolUse))
	if result == "" {
		t.Fatal("expected non-empty result for Claude tool_use event")
	}
	if !strings.Contains(result, "Read") {
		t.Errorf("result should contain tool name, got: %s", result)
	}
	if !strings.Contains(result, "/project/go.mod") {
		t.Errorf("result should contain file path, got: %s", result)
	}
	if !strings.Contains(result, "✓") {
		t.Errorf("result should contain success icon, got: %s", result)
	}
}

func TestFormatLogLine_ClaudeText(t *testing.T) {
	result := FormatLogLine([]byte(claudeAssistantText))
	if result == "" {
		t.Fatal("expected non-empty result for Claude text event")
	}
	if !strings.Contains(result, "github.com/baiirun/aetherflow") {
		t.Errorf("result should contain text, got: %s", result)
	}
}

func TestFormatLogLine_ClaudeResult(t *testing.T) {
	result := FormatLogLine([]byte(claudeResultEvent))
	if result == "" {
		t.Fatal("expected non-empty result for Claude result event")
	}
	if !strings.Contains(result, "done") {
		t.Errorf("result should contain done marker, got: %s", result)
	}
	if !strings.Contains(result, "5.3s") {
		t.Errorf("result should contain duration, got: %s", result)
	}
	if !strings.Contains(result, "2 turns") {
		t.Errorf("result should contain turn count, got: %s", result)
	}
}

func TestFormatLogLine_ClaudeSystemHidden(t *testing.T) {
	result := FormatLogLine([]byte(claudeSystemEvent))
	if result != "" {
		t.Errorf("Claude system event should be hidden, got: %s", result)
	}
}

func TestFormatLogLine_ClaudeUserHidden(t *testing.T) {
	result := FormatLogLine([]byte(claudeUserToolResult))
	if result != "" {
		t.Errorf("Claude user (tool result) event should be hidden, got: %s", result)
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
