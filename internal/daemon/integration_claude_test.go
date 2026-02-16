package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegrationClaudeRealOutput runs the parsers and formatters against
// real captured Claude Code stream-json output. The test fixture was
// captured by running claude --print --output-format stream-json --verbose
// against the aetherflow project.
func TestIntegrationClaudeRealOutput(t *testing.T) {
	testFile := filepath.Join("testdata", "claude-stream.jsonl")

	ctx := context.Background()

	// --- Format detection ---
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("reading test file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d", len(lines))
	}

	for i, line := range lines {
		format := DetectLogFormat([]byte(line))
		if format != LogFormatClaude {
			t.Errorf("line %d: expected Claude format, got %v", i, format)
		}
	}
	t.Logf("format detection: all %d lines detected as Claude", len(lines))

	// --- ParseToolCalls ---
	calls, total, err := ParseToolCalls(ctx, testFile, 100)
	if err != nil {
		t.Fatalf("ParseToolCalls: %v", err)
	}
	t.Logf("ParseToolCalls: %d calls, %d total lines", len(calls), total)

	if len(calls) == 0 {
		t.Error("expected at least one tool call (Read)")
	}

	// The test prompt asks to read go.mod, so we expect a Read tool call.
	foundRead := false
	for _, call := range calls {
		t.Logf("  tool=%s status=%s input=%s", call.Tool, call.Status, call.Input)
		if call.Tool == "Read" {
			foundRead = true
			if call.Status != "completed" {
				t.Errorf("Read tool should be completed, got %s", call.Status)
			}
			if !strings.Contains(call.Input, "go.mod") {
				t.Errorf("Read tool key input should contain go.mod, got %s", call.Input)
			}
		}
	}
	if !foundRead {
		t.Error("expected a Read tool call in the output")
	}

	// --- ParseSessionID ---
	sessionID, err := ParseSessionID(ctx, testFile)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	if sessionID == "" {
		t.Error("expected non-empty session ID")
	}
	t.Logf("ParseSessionID: %s", sessionID)

	// --- FormatLogLine ---
	var formatted, hidden int
	for _, line := range lines {
		result := FormatLogLine([]byte(line))
		if result == "" {
			hidden++
		} else {
			formatted++
			t.Logf("  formatted: %s", result)
		}
	}
	t.Logf("FormatLogLine: %d formatted, %d hidden", formatted, hidden)

	// system and user events should be hidden; assistant and result should be formatted.
	if formatted < 2 {
		t.Errorf("expected at least 2 formatted lines (assistant + result), got %d", formatted)
	}
	if hidden < 1 {
		t.Errorf("expected at least 1 hidden line (system), got %d", hidden)
	}
}
