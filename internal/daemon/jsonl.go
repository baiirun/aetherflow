package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ToolCall is a single tool invocation extracted from a JSONL log line.
type ToolCall struct {
	Timestamp  time.Time `json:"timestamp"`
	Tool       string    `json:"tool"`
	Title      string    `json:"title,omitempty"`
	Input      string    `json:"input"`
	Status     string    `json:"status"`
	DurationMs int       `json:"duration_ms,omitempty"`
}

// jsonlLine is the sparse parse target for an opencode --format json line.
// Only the fields needed for tool call extraction are included.
type jsonlLine struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"` // Unix millis
	SessionID string `json:"sessionID"`
	Part      struct {
		Tool  string `json:"tool"`
		Title string `json:"title"`
		State struct {
			Status string          `json:"status"`
			Input  json.RawMessage `json:"input"`
			Time   struct {
				Start int64 `json:"start"` // Unix millis
				End   int64 `json:"end"`   // Unix millis
			} `json:"time"`
		} `json:"state"`
	} `json:"part"`
}

// ParseToolCalls reads a JSONL log file and extracts tool_use events.
// Returns up to limit most recent tool calls (0 means all), and the count
// of lines that were skipped due to parse errors. Partial logs from crashes
// are expected, so malformed lines are counted rather than causing a failure.
func ParseToolCalls(ctx context.Context, path string, limit int) ([]ToolCall, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("opening log file: %w", err)
	}
	defer f.Close()

	var calls []ToolCall
	var skipped int
	scanner := bufio.NewScanner(f)
	// Increase buffer size — tool results can be large (file contents in output).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, skipped, err
		}

		var line jsonlLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			skipped++
			continue
		}
		if line.Type != "tool_use" {
			continue
		}

		tc := ToolCall{
			Timestamp: time.UnixMilli(line.Timestamp),
			Tool:      line.Part.Tool,
			Title:     line.Part.Title,
			Status:    line.Part.State.Status,
			Input:     extractKeyInput(line.Part.Tool, line.Part.State.Input),
		}

		if line.Part.State.Time.Start > 0 && line.Part.State.Time.End > 0 {
			tc.DurationMs = int(line.Part.State.Time.End - line.Part.State.Time.Start)
		}

		calls = append(calls, tc)
	}

	if err := scanner.Err(); err != nil {
		return nil, skipped, fmt.Errorf("reading log file: %w", err)
	}

	if limit > 0 && len(calls) > limit {
		calls = calls[len(calls)-limit:]
	}

	return calls, skipped, nil
}

// extractKeyInput pulls the most relevant field from a tool's input JSON.
// Each tool has a different input shape; we extract the one field that gives
// the best at-a-glance summary.
func extractKeyInput(tool string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}

	// Try tool-specific key fields in order of usefulness.
	switch tool {
	case "read", "edit", "write":
		return unquoteField(m, "filePath")
	case "bash":
		return unquoteField(m, "command")
	case "glob", "grep":
		return unquoteField(m, "pattern")
	case "task":
		return unquoteField(m, "description")
	case "skill":
		return unquoteField(m, "name")
	default:
		// For unknown tools, try common fields.
		for _, key := range []string{"filePath", "command", "url", "query", "pattern", "description", "name"} {
			if v := unquoteField(m, key); v != "" {
				return v
			}
		}
		return ""
	}
}

// unquoteField extracts and unquotes a string field from a JSON object.
func unquoteField(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// ParseSessionID reads the first JSONL line from a log file and extracts the
// sessionID. Returns empty string and nil error if the file doesn't exist or
// the sessionID field is absent (these are not errors - the field may not be
// available yet). Returns an error if the file exists but is malformed.
func ParseSessionID(ctx context.Context, logFile string) (string, error) {
	f, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // Not an error - file doesn't exist yet
		}
		return "", fmt.Errorf("opening log file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading first line: %w", err)
		}
		return "", nil // Empty file is not an error
	}

	// Check context before parsing
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var line jsonlLine
	if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
		return "", fmt.Errorf("parsing first line: %w", err)
	}

	return line.SessionID, nil
}
