package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Sample Claude Code stream-json events captured from real output.
const (
	claudeSystemEvent = `{"type":"system","subtype":"init","cwd":"/project","session_id":"d390a9a5-8962-423c-888d-c9febf1d48f6","tools":["Task","Bash","Read","Edit","Write","Glob","Grep"],"model":"claude-opus-4-6"}`

	claudeAssistantToolUse = `{"type":"assistant","message":{"model":"claude-opus-4-6","id":"msg_01PZaY5N","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_01MTPx","name":"Read","input":{"file_path":"/project/go.mod"}}],"stop_reason":null},"session_id":"d390a9a5-8962-423c-888d-c9febf1d48f6"}`

	claudeAssistantMultiTool = `{"type":"assistant","message":{"model":"claude-opus-4-6","id":"msg_02","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"command":"go test ./..."}},{"type":"tool_use","id":"toolu_02","name":"Read","input":{"file_path":"/project/main.go"}}],"stop_reason":null},"session_id":"d390a9a5-8962-423c-888d-c9febf1d48f6"}`

	claudeAssistantText = `{"type":"assistant","message":{"model":"claude-opus-4-6","id":"msg_013cp","type":"message","role":"assistant","content":[{"type":"text","text":"The module name is github.com/baiirun/aetherflow."}],"stop_reason":null},"session_id":"d390a9a5-8962-423c-888d-c9febf1d48f6"}`

	claudeUserToolResult = `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01MTPx","type":"tool_result","content":"module github.com/baiirun/aetherflow"}]},"session_id":"d390a9a5-8962-423c-888d-c9febf1d48f6"}`

	claudeResultEvent = `{"type":"result","subtype":"success","is_error":false,"duration_ms":5339,"num_turns":2,"result":"The module name is github.com/baiirun/aetherflow.","session_id":"d390a9a5-8962-423c-888d-c9febf1d48f6","total_cost_usd":0.036763}`
)

func TestDetectLogFormat(t *testing.T) {
	tests := []struct {
		name string
		line string
		want LogFormat
	}{
		{
			name: "opencode step_start",
			line: `{"type":"step_start","timestamp":1770499337321,"sessionID":"ses_1","part":{"type":"step-start"}}`,
			want: LogFormatOpencode,
		},
		{
			name: "opencode tool_use",
			line: `{"type":"tool_use","timestamp":1770499345000,"sessionID":"ses_1","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/foo"}}}}`,
			want: LogFormatOpencode,
		},
		{
			name: "claude system init",
			line: claudeSystemEvent,
			want: LogFormatClaude,
		},
		{
			name: "claude assistant",
			line: claudeAssistantToolUse,
			want: LogFormatClaude,
		},
		{
			name: "claude user",
			line: claudeUserToolResult,
			want: LogFormatClaude,
		},
		{
			name: "claude result",
			line: claudeResultEvent,
			want: LogFormatClaude,
		},
		{
			name: "invalid json",
			line: `not json at all`,
			want: LogFormatUnknown,
		},
		{
			name: "empty json object",
			line: `{}`,
			want: LogFormatUnknown,
		},
		{
			name: "unknown type",
			line: `{"type":"something_else"}`,
			want: LogFormatUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectLogFormat([]byte(tt.line))
			if got != tt.want {
				t.Errorf("DetectLogFormat() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseClaudeToolCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		claudeSystemEvent,
		claudeAssistantToolUse,
		claudeUserToolResult,
		claudeAssistantText,
		claudeResultEvent,
	}

	writeLines(t, path, lines)

	calls, skipped, err := parseClaudeToolCalls(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("parseClaudeToolCalls: %v", err)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}

	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}

	if calls[0].Tool != "Read" {
		t.Errorf("call[0].Tool = %q, want %q", calls[0].Tool, "Read")
	}
	if calls[0].Input != "/project/go.mod" {
		t.Errorf("call[0].Input = %q, want %q", calls[0].Input, "/project/go.mod")
	}
	if calls[0].Status != "completed" {
		t.Errorf("call[0].Status = %q, want %q", calls[0].Status, "completed")
	}
}

func TestParseClaudeToolCallsMultiTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		claudeSystemEvent,
		claudeAssistantMultiTool,
		claudeResultEvent,
	}

	writeLines(t, path, lines)

	calls, _, err := parseClaudeToolCalls(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("parseClaudeToolCalls: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2 (multi-tool message)", len(calls))
	}

	if calls[0].Tool != "Bash" {
		t.Errorf("call[0].Tool = %q, want %q", calls[0].Tool, "Bash")
	}
	if calls[0].Input != "go test ./..." {
		t.Errorf("call[0].Input = %q, want %q", calls[0].Input, "go test ./...")
	}

	if calls[1].Tool != "Read" {
		t.Errorf("call[1].Tool = %q, want %q", calls[1].Tool, "Read")
	}
	if calls[1].Input != "/project/main.go" {
		t.Errorf("call[1].Input = %q, want %q", calls[1].Input, "/project/main.go")
	}
}

func TestParseClaudeToolCallsLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		claudeSystemEvent,
		claudeAssistantMultiTool, // 2 tool calls
		claudeAssistantToolUse,   // 1 tool call
		claudeResultEvent,
	}

	writeLines(t, path, lines)

	calls, _, err := parseClaudeToolCalls(context.Background(), path, 2)
	if err != nil {
		t.Fatalf("parseClaudeToolCalls: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2 (limited)", len(calls))
	}

	// Should return the last 2: Read from multi-tool, Read from single.
	if calls[0].Tool != "Read" {
		t.Errorf("call[0].Tool = %q, want %q", calls[0].Tool, "Read")
	}
	if calls[0].Input != "/project/main.go" {
		t.Errorf("call[0].Input = %q, want %q", calls[0].Input, "/project/main.go")
	}
}

func TestParseClaudeToolCallsMissingFile(t *testing.T) {
	calls, skipped, err := parseClaudeToolCalls(context.Background(), "/nonexistent/ts-abc.jsonl", 0)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected empty calls for missing file, got %d", len(calls))
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
}

func TestParseClaudeSessionID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		claudeSystemEvent,
		claudeAssistantToolUse,
	}

	writeLines(t, path, lines)

	got, err := parseClaudeSessionID(context.Background(), path)
	if err != nil {
		t.Fatalf("parseClaudeSessionID: %v", err)
	}

	want := "d390a9a5-8962-423c-888d-c9febf1d48f6"
	if got != want {
		t.Errorf("parseClaudeSessionID = %q, want %q", got, want)
	}
}

func TestParseClaudeSessionIDMissingFile(t *testing.T) {
	got, err := parseClaudeSessionID(context.Background(), "/nonexistent/file.jsonl")
	if err != nil {
		t.Errorf("parseClaudeSessionID on missing file returned error: %v", err)
	}
	if got != "" {
		t.Errorf("parseClaudeSessionID on missing file = %q, want empty string", got)
	}
}

func TestParseClaudeSessionIDEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")

	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := parseClaudeSessionID(context.Background(), path)
	if err != nil {
		t.Errorf("parseClaudeSessionID on empty file returned error: %v", err)
	}
	if got != "" {
		t.Errorf("parseClaudeSessionID on empty file = %q, want empty string", got)
	}
}

// TestParseToolCallsRoutesToClaude verifies that the public ParseToolCalls
// function auto-detects Claude Code format and dispatches correctly.
func TestParseToolCallsRoutesToClaude(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		claudeSystemEvent,
		claudeAssistantToolUse,
		claudeResultEvent,
	}

	writeLines(t, path, lines)

	// Use the public function — it should auto-detect Claude format.
	calls, _, err := ParseToolCalls(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if calls[0].Tool != "Read" {
		t.Errorf("call[0].Tool = %q, want %q", calls[0].Tool, "Read")
	}
}

// TestParseSessionIDRoutesToClaude verifies the public ParseSessionID
// function auto-detects Claude Code format.
func TestParseSessionIDRoutesToClaude(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		claudeSystemEvent,
	}

	writeLines(t, path, lines)

	got, err := ParseSessionID(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}

	want := "d390a9a5-8962-423c-888d-c9febf1d48f6"
	if got != want {
		t.Errorf("ParseSessionID = %q, want %q", got, want)
	}
}

func TestExtractKeyInputClaudeSnakeCase(t *testing.T) {
	// Claude Code uses file_path (snake_case), not filePath.
	tests := []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{"Read file_path", "Read", `{"file_path":"/foo/bar.go"}`, "/foo/bar.go"},
		{"Edit file_path", "Edit", `{"file_path":"/foo/bar.go","old_string":"a","new_string":"b"}`, "/foo/bar.go"},
		{"Bash command", "Bash", `{"command":"go test ./..."}`, "go test ./..."},
		{"Glob pattern", "Glob", `{"pattern":"**/*.go"}`, "**/*.go"},
		{"Grep pattern", "Grep", `{"pattern":"func main"}`, "func main"},
		{"Task description", "Task", `{"description":"review code"}`, "review code"},
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
