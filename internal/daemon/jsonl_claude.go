package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Claude Code stream-json event structure.
//
// Claude Code outputs 4 event types:
//   - system (subtype init): session metadata, tools, model
//   - assistant: wraps a full Anthropic message with content[] blocks
//   - user: tool results
//   - result: final summary with cost, duration, usage
//
// Tool calls appear inside assistant events as content blocks with type "tool_use".

// claudeEvent is the sparse parse target for a Claude Code stream-json line.
type claudeEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Message   struct {
		Content []claudeContentBlock `json:"content"`
	} `json:"message"`
	// result event fields
	DurationMs int `json:"duration_ms"`
}

// claudeContentBlock represents an item in an assistant message's content array.
type claudeContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Text  string          `json:"text"`
	Input json.RawMessage `json:"input"`
}

// parseClaudeToolCalls extracts tool invocations from a Claude Code JSONL log.
// Tool calls appear as content blocks (type "tool_use") inside "assistant" events.
// Tool results appear in the following "user" event with matching tool_use_id.
//
// Since Claude Code doesn't include per-tool timing or completion status in
// the stream-json output (tools are always completed by the time they appear),
// DurationMs is 0 and Status is "completed" for all calls.
func parseClaudeToolCalls(ctx context.Context, path string, limit int) ([]ToolCall, int, error) {
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
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, skipped, err
		}

		var ev claudeEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			skipped++
			continue
		}

		// Only assistant events contain tool_use content blocks.
		if ev.Type != "assistant" {
			continue
		}

		for _, block := range ev.Message.Content {
			if block.Type != "tool_use" {
				continue
			}

			tc := ToolCall{
				Timestamp: time.Now(), // Claude stream-json doesn't include per-event timestamps
				Tool:      block.Name,
				Status:    "completed", // tools appear only after completion in stream-json
				Input:     extractKeyInput(block.Name, block.Input),
			}

			calls = append(calls, tc)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, skipped, fmt.Errorf("reading log file: %w", err)
	}

	if limit > 0 && len(calls) > limit {
		calls = calls[len(calls)-limit:]
	}

	return calls, skipped, nil
}

// parseClaudeSessionID extracts the session ID from a Claude Code JSONL log.
// Claude Code includes session_id on each event. The first event (type "system")
// is the most reliable source.
func parseClaudeSessionID(ctx context.Context, logFile string) (string, error) {
	f, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("opening log file: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		var ev claudeEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}

		if ev.SessionID != "" {
			return ev.SessionID, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading log file: %w", err)
	}

	return "", nil
}
