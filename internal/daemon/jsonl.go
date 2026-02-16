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

// LogFormat identifies the JSONL output format of an agent runtime.
type LogFormat int

const (
	// LogFormatUnknown means the format could not be determined.
	LogFormatUnknown LogFormat = iota
	// LogFormatOpencode is the opencode --format json output.
	LogFormatOpencode
	// LogFormatClaude is the Claude Code --output-format stream-json output.
	LogFormatClaude
)

// DetectLogFormat determines the log format from a single JSONL line.
// opencode lines always have a top-level "sessionID" key (camelCase).
// Claude Code lines use "session_id" (snake_case) and have a distinct
// "type" vocabulary ("system", "assistant", "user", "result").
func DetectLogFormat(line []byte) LogFormat {
	// Fast probe: check for opencode's camelCase sessionID.
	var opcodeProbe struct {
		SessionID *string `json:"sessionID"`
	}
	if json.Unmarshal(line, &opcodeProbe) == nil && opcodeProbe.SessionID != nil {
		return LogFormatOpencode
	}

	// Check for Claude Code's event types.
	var claudeProbe struct {
		Type      string  `json:"type"`
		SessionID *string `json:"session_id"`
	}
	if json.Unmarshal(line, &claudeProbe) == nil {
		switch claudeProbe.Type {
		case "system", "assistant", "user", "result":
			return LogFormatClaude
		}
	}

	return LogFormatUnknown
}

// detectFileFormat opens a file, reads its first line, and returns the
// detected log format. Returns LogFormatUnknown (not an error) when the
// file is empty. Returns nil error and LogFormatUnknown for missing files
// so callers can treat both as "no data yet."
func detectFileFormat(path string) (LogFormat, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LogFormatUnknown, nil
		}
		return LogFormatUnknown, fmt.Errorf("opening log file: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return LogFormatUnknown, nil // empty file
	}

	return DetectLogFormat(scanner.Bytes()), nil
}

// ParseToolCalls reads a JSONL log file and extracts tool invocations.
// The log format is auto-detected from the file content so this function
// works transparently with both opencode and Claude Code logs.
// Returns up to limit most recent tool calls (0 means all), and the count
// of lines that were skipped due to parse errors.
func ParseToolCalls(ctx context.Context, path string, limit int) ([]ToolCall, int, error) {
	format, err := detectFileFormat(path)
	if err != nil {
		return nil, 0, err
	}

	switch format {
	case LogFormatClaude:
		return parseClaudeToolCalls(ctx, path, limit)
	case LogFormatOpencode:
		return parseOpencodeToolCalls(ctx, path, limit)
	default:
		// Unknown or empty — try opencode (the original default).
		return parseOpencodeToolCalls(ctx, path, limit)
	}
}

// ParseSessionID reads the first JSONL line from a log file and extracts
// the session ID. Auto-detects format. Returns empty string and nil error
// if the file doesn't exist or the session ID is absent.
func ParseSessionID(ctx context.Context, logFile string) (string, error) {
	format, err := detectFileFormat(logFile)
	if err != nil {
		return "", err
	}

	switch format {
	case LogFormatClaude:
		return parseClaudeSessionID(ctx, logFile)
	case LogFormatOpencode:
		return parseOpencodeSessionID(ctx, logFile)
	default:
		return parseOpencodeSessionID(ctx, logFile)
	}
}

// --- opencode format ---

// opcodeJSONLLine is the sparse parse target for an opencode --format json line.
// Only the fields needed for tool call extraction are included.
type opcodeJSONLLine struct {
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

func parseOpencodeToolCalls(ctx context.Context, path string, limit int) ([]ToolCall, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("opening log file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var calls []ToolCall
	var skipped int
	scanner := bufio.NewScanner(f)
	// Increase buffer size — tool results can be large (file contents in output).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, skipped, err
		}

		var line opcodeJSONLLine
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
// the best at-a-glance summary. Works for both opencode and Claude Code
// since both use the same tool names and input structures.
func extractKeyInput(tool string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}

	// Try tool-specific key fields in order of usefulness.
	// Both opencode and Claude Code use these tool names.
	switch tool {
	case "Read", "Edit", "Write", "read", "edit", "write":
		// Claude Code uses file_path (snake_case), opencode uses filePath.
		if v := unquoteField(m, "file_path"); v != "" {
			return v
		}
		return unquoteField(m, "filePath")
	case "Bash", "bash":
		return unquoteField(m, "command")
	case "Glob", "Grep", "glob", "grep":
		return unquoteField(m, "pattern")
	case "Task", "task":
		return unquoteField(m, "description")
	case "Skill", "skill":
		return unquoteField(m, "name")
	default:
		// For unknown tools, try common fields.
		for _, key := range []string{"file_path", "filePath", "command", "url", "query", "pattern", "description", "name"} {
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

func parseOpencodeSessionID(ctx context.Context, logFile string) (string, error) {
	f, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // Not an error - file doesn't exist yet
		}
		return "", fmt.Errorf("opening log file: %w", err)
	}
	defer func() { _ = f.Close() }()

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

	var line opcodeJSONLLine
	if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
		return "", fmt.Errorf("parsing first line: %w", err)
	}

	return line.SessionID, nil
}
