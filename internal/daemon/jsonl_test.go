package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseToolCallsHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		`{"type":"step_start","timestamp":1770499337321,"sessionID":"ses_1","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1770499345000,"sessionID":"ses_1","part":{"tool":"read","title":"go.mod","state":{"status":"completed","input":{"filePath":"/project/go.mod"},"time":{"start":1770499344000,"end":1770499345000}}}}`,
		`{"type":"text","timestamp":1770499346000,"sessionID":"ses_1","part":{"type":"text","text":"The module is foo."}}`,
		`{"type":"tool_use","timestamp":1770499347000,"sessionID":"ses_1","part":{"tool":"bash","title":"","state":{"status":"completed","input":{"command":"go test ./..."},"time":{"start":1770499346500,"end":1770499347000}}}}`,
		`{"type":"tool_use","timestamp":1770499348000,"sessionID":"ses_1","part":{"tool":"edit","title":"main.go","state":{"status":"completed","input":{"filePath":"/project/main.go","oldString":"foo","newString":"bar"},"time":{"start":1770499347500,"end":1770499348000}}}}`,
		`{"type":"step_finish","timestamp":1770499349000,"sessionID":"ses_1","part":{"type":"step-finish"}}`,
	}

	writeLines(t, path, lines)

	calls, skipped, err := ParseToolCalls(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}

	if len(calls) != 3 {
		t.Fatalf("got %d tool calls, want 3", len(calls))
	}

	// First call: read
	if calls[0].Tool != "read" {
		t.Errorf("call[0].Tool = %q, want %q", calls[0].Tool, "read")
	}
	if calls[0].Input != "/project/go.mod" {
		t.Errorf("call[0].Input = %q, want %q", calls[0].Input, "/project/go.mod")
	}
	if calls[0].Status != "completed" {
		t.Errorf("call[0].Status = %q, want %q", calls[0].Status, "completed")
	}
	if calls[0].DurationMs != 1000 {
		t.Errorf("call[0].DurationMs = %d, want 1000", calls[0].DurationMs)
	}
	if calls[0].Title != "go.mod" {
		t.Errorf("call[0].Title = %q, want %q", calls[0].Title, "go.mod")
	}

	// Second call: bash
	if calls[1].Tool != "bash" {
		t.Errorf("call[1].Tool = %q, want %q", calls[1].Tool, "bash")
	}
	if calls[1].Input != "go test ./..." {
		t.Errorf("call[1].Input = %q, want %q", calls[1].Input, "go test ./...")
	}

	// Third call: edit
	if calls[2].Tool != "edit" {
		t.Errorf("call[2].Tool = %q, want %q", calls[2].Tool, "edit")
	}
	if calls[2].Input != "/project/main.go" {
		t.Errorf("call[2].Input = %q, want %q", calls[2].Input, "/project/main.go")
	}
}

func TestParseToolCallsLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		`{"type":"tool_use","timestamp":1770499345000,"sessionID":"ses_1","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/a"},"time":{"start":1,"end":2}}}}`,
		`{"type":"tool_use","timestamp":1770499346000,"sessionID":"ses_1","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/b"},"time":{"start":1,"end":2}}}}`,
		`{"type":"tool_use","timestamp":1770499347000,"sessionID":"ses_1","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/c"},"time":{"start":1,"end":2}}}}`,
	}

	writeLines(t, path, lines)

	calls, _, err := ParseToolCalls(context.Background(), path, 2)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2 (limited)", len(calls))
	}

	// Should return the last 2 (most recent).
	if calls[0].Input != "/b" {
		t.Errorf("call[0].Input = %q, want %q (second tool call)", calls[0].Input, "/b")
	}
	if calls[1].Input != "/c" {
		t.Errorf("call[1].Input = %q, want %q (third tool call)", calls[1].Input, "/c")
	}
}

func TestParseToolCallsMissingFile(t *testing.T) {
	calls, skipped, err := ParseToolCalls(context.Background(), "/nonexistent/ts-abc.jsonl", 0)
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

func TestParseToolCallsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		`not json at all`,
		`{"type":"tool_use","timestamp":1770499345000,"sessionID":"ses_1","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/good"},"time":{"start":1,"end":2}}}}`,
		`{"truncated json`,
		`{"type":"tool_use","timestamp":1770499346000,"sessionID":"ses_1","part":{"tool":"bash","state":{"status":"completed","input":{"command":"ls"},"time":{"start":1,"end":2}}}}`,
	}

	writeLines(t, path, lines)

	calls, skipped, err := ParseToolCalls(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}

	// Should have parsed the 2 valid tool_use lines, skipping the malformed ones.
	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2 (skipping malformed)", len(calls))
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2 (two malformed lines)", skipped)
	}
}

func TestParseToolCallsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}

	calls, _, err := ParseToolCalls(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected empty calls for empty file, got %d", len(calls))
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
		{"unknown with filePath", "webfetch", `{"url":"https://example.com"}`, "https://example.com"},
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

func TestParseToolCallsNoDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	// Tool call with no time fields — DurationMs should be 0.
	lines := []string{
		`{"type":"tool_use","timestamp":1770499345000,"sessionID":"ses_1","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/foo"}}}}`,
	}

	writeLines(t, path, lines)

	calls, _, err := ParseToolCalls(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].DurationMs != 0 {
		t.Errorf("DurationMs = %d, want 0 (no time data)", calls[0].DurationMs)
	}
}

func TestParseToolCallsCancelledContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		`{"type":"tool_use","timestamp":1770499345000,"sessionID":"ses_1","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/a"},"time":{"start":1,"end":2}}}}`,
		`{"type":"tool_use","timestamp":1770499346000,"sessionID":"ses_1","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/b"},"time":{"start":1,"end":2}}}}`,
	}

	writeLines(t, path, lines)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := ParseToolCalls(ctx, path, 0)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestParseSessionIDHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ts-abc.jsonl")

	lines := []string{
		`{"type":"step_start","timestamp":1770499337321,"sessionID":"ses_3ba7385ddffeRK9Kk26WLuA3XA","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1770499345000,"sessionID":"ses_3ba7385ddffeRK9Kk26WLuA3XA","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/project/go.mod"}}}}`,
	}

	writeLines(t, path, lines)

	got := ParseSessionID(path)
	want := "ses_3ba7385ddffeRK9Kk26WLuA3XA"

	if got != want {
		t.Errorf("ParseSessionID = %q, want %q", got, want)
	}
}

func TestParseSessionIDMissingFile(t *testing.T) {
	got := ParseSessionID("/nonexistent/file.jsonl")
	if got != "" {
		t.Errorf("ParseSessionID on missing file = %q, want empty string", got)
	}
}

func TestParseSessionIDEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")

	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}

	got := ParseSessionID(path)
	if got != "" {
		t.Errorf("ParseSessionID on empty file = %q, want empty string", got)
	}
}

func TestParseSessionIDMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "malformed.jsonl")

	lines := []string{`not valid json at all`}
	writeLines(t, path, lines)

	got := ParseSessionID(path)
	if got != "" {
		t.Errorf("ParseSessionID on malformed JSON = %q, want empty string", got)
	}
}

func TestParseSessionIDNoSessionField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-session.jsonl")

	// Valid JSON but no sessionID field
	lines := []string{`{"type":"step_start","timestamp":1770499337321}`}
	writeLines(t, path, lines)

	got := ParseSessionID(path)
	if got != "" {
		t.Errorf("ParseSessionID on line without sessionID = %q, want empty string", got)
	}
}

// writeLines writes JSONL lines to a file.
func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, line := range lines {
		f.WriteString(line + "\n")
	}
}
