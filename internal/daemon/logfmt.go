// Package daemon provides the log formatter for human-readable JSONL output.
package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LogEvent is a parsed JSONL log line with all event types supported.
type LogEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"` // Unix millis
	Part      struct {
		Type string `json:"type"`
		Text string `json:"text"`

		// Tool fields (type == "tool" or "tool_use")
		Tool  string `json:"tool"`
		State struct {
			Status string          `json:"status"`
			Input  json.RawMessage `json:"input"`
			Output string          `json:"output"`
			Title  string          `json:"title"`
			Time   struct {
				Start int64 `json:"start"`
				End   int64 `json:"end"`
			} `json:"time"`
		} `json:"state"`

		// Step finish fields
		Reason string  `json:"reason"`
		Cost   float64 `json:"cost"`
		Tokens struct {
			Input     int `json:"input"`
			Output    int `json:"output"`
			Reasoning int `json:"reasoning"`
			Cache     struct {
				Read  int `json:"read"`
				Write int `json:"write"`
			} `json:"cache"`
		} `json:"tokens"`
	} `json:"part"`
}

// FormatLogLine parses a raw JSONL line and returns a human-readable string.
// Auto-detects the log format (opencode vs Claude Code) and dispatches to
// the appropriate formatter. Returns empty string for events that should
// be hidden (e.g. step_start, system init).
func FormatLogLine(raw []byte) string {
	format := DetectLogFormat(raw)
	switch format {
	case LogFormatClaude:
		return formatClaudeLogLine(raw)
	default:
		return formatOpencodeLogLine(raw)
	}
}

func formatOpencodeLogLine(raw []byte) string {
	var ev LogEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return ""
	}

	ts := time.UnixMilli(ev.Timestamp).Format("15:04:05")

	switch ev.Type {
	case "text":
		return formatText(ts, ev)
	case "tool_use":
		return formatToolUse(ts, ev)
	case "step_finish":
		return formatStepFinish(ts, ev)
	default:
		// step_start, etc. — skip
		return ""
	}
}

// claudeLogEvent is the parse target for Claude Code stream-json formatting.
type claudeLogEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Text  string          `json:"text"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			OutputTokens              int `json:"output_tokens"`
			CacheReadInputTokens      int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens  int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	DurationMs int     `json:"duration_ms"`
	TotalCost  float64 `json:"total_cost_usd"`
	NumTurns   int     `json:"num_turns"`
}

func formatClaudeLogLine(raw []byte) string {
	var ev claudeLogEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return ""
	}

	ts := time.Now().Format("15:04:05")

	switch ev.Type {
	case "assistant":
		return formatClaudeAssistant(ts, ev)
	case "result":
		return formatClaudeResult(ts, ev)
	default:
		// system, user (tool results) — skip for display
		return ""
	}
}

func formatClaudeAssistant(ts string, ev claudeLogEvent) string {
	var parts []string

	for _, block := range ev.Message.Content {
		switch block.Type {
		case "text":
			text := strings.TrimSpace(block.Text)
			if text != "" {
				parts = append(parts, fmt.Sprintf("%s%s%s  %s", ansiDim, ts, ansiReset, text))
			}
		case "tool_use":
			input := extractKeyInput(block.Name, block.Input)
			statusIcon := fmt.Sprintf("%s✓%s", ansiGreen, ansiReset)

			parts = append(parts, fmt.Sprintf("%s%s%s  %s%s%s %s%s",
				ansiDim, ts, ansiReset,
				ansiCyan, block.Name, ansiReset,
				statusIcon,
				truncateStr(input, 80),
			))
		}
	}

	return strings.Join(parts, "\n")
}

func formatClaudeResult(ts string, ev claudeLogEvent) string {
	if ev.DurationMs == 0 && ev.NumTurns == 0 {
		return ""
	}

	return fmt.Sprintf("%s%s  ── done ── %s  %d turns  $%.4f%s",
		ansiDim, ts,
		formatMs(int64(ev.DurationMs)),
		ev.NumTurns,
		ev.TotalCost,
		ansiReset,
	)
}

// ANSI color helpers for terminal output.
const (
	ansiReset   = "\033[0m"
	ansiDim     = "\033[2m"
	ansiBold    = "\033[1m"
	ansiCyan    = "\033[36m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiRed     = "\033[31m"
	ansiMagenta = "\033[35m"
	ansiBlue    = "\033[34m"
)

func formatText(ts string, ev LogEvent) string {
	text := strings.TrimSpace(ev.Part.Text)
	if text == "" {
		return ""
	}
	return fmt.Sprintf("%s%s%s  %s", ansiDim, ts, ansiReset, text)
}

func formatToolUse(ts string, ev LogEvent) string {
	tool := ev.Part.Tool
	status := ev.Part.State.Status
	title := ev.Part.State.Title
	input := extractKeyInput(tool, ev.Part.State.Input)

	// Use title if available, otherwise input summary.
	label := input
	if title != "" {
		label = title
	}

	// Duration.
	dur := ""
	if ev.Part.State.Time.Start > 0 && ev.Part.State.Time.End > 0 {
		ms := ev.Part.State.Time.End - ev.Part.State.Time.Start
		dur = fmt.Sprintf(" %s(%s)%s", ansiDim, formatMs(ms), ansiReset)
	}

	// Status indicator.
	statusIcon := ""
	switch status {
	case "completed":
		statusIcon = fmt.Sprintf("%s✓%s", ansiGreen, ansiReset)
	case "error":
		statusIcon = fmt.Sprintf("%s✗%s", ansiRed, ansiReset)
	case "running":
		statusIcon = fmt.Sprintf("%s…%s", ansiYellow, ansiReset)
	}

	return fmt.Sprintf("%s%s%s  %s%s%s %s%s%s%s",
		ansiDim, ts, ansiReset,
		ansiCyan, tool, ansiReset,
		statusIcon,
		truncateStr(label, 80),
		dur,
		formatToolOutput(ev),
	)
}

// formatToolOutput returns a compact summary of tool output for select tools.
func formatToolOutput(ev LogEvent) string {
	if ev.Part.State.Status != "completed" {
		return ""
	}

	output := ev.Part.State.Output
	if output == "" {
		return ""
	}

	// For bash, show first line of output (truncated).
	if ev.Part.Tool == "bash" {
		first := firstLine(output)
		if first != "" {
			return fmt.Sprintf("\n    %s→ %s%s", ansiDim, truncateStr(first, 100), ansiReset)
		}
	}

	return ""
}

func formatStepFinish(ts string, ev LogEvent) string {
	tokens := ev.Part.Tokens
	total := tokens.Input + tokens.Output + tokens.Reasoning
	cached := tokens.Cache.Read

	if total == 0 && cached == 0 {
		return ""
	}

	return fmt.Sprintf("%s%s  ── step ── %s out  %s cached  reason: %s%s",
		ansiDim, ts,
		formatTokens(tokens.Output),
		formatTokens(cached),
		ev.Part.Reason,
		ansiReset,
	)
}

func formatMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	s := float64(ms) / 1000
	if s < 60 {
		return fmt.Sprintf("%.1fs", s)
	}
	m := int(s) / 60
	rs := int(s) % 60
	return fmt.Sprintf("%dm%ds", m, rs)
}

func formatTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
