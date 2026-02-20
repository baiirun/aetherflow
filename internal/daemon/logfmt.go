package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// LogEvent is the internal formatting target for event display.
// FormatEvent parses SessionEvent data into this structure to reuse
// the shared formatting functions (formatText, formatToolUse, formatStepFinish).
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

// FormatEvent formats a SessionEvent from the plugin event pipeline into a
// human-readable string. Returns empty string for events that should be hidden.
//
// Plugin events use "message.part.updated" with data: {"part": {...}} where
// the part has type "text", "tool", "step-start", "step-finish", etc. This
// function handles text, tool, and step-finish event types from the plugin
// event shape.
func FormatEvent(ev SessionEvent) string {
	if ev.EventType != "message.part.updated" {
		return ""
	}
	if len(ev.Data) == 0 {
		return ""
	}

	var envelope struct {
		Part json.RawMessage `json:"part"`
	}
	if err := json.Unmarshal(ev.Data, &envelope); err != nil {
		slog.Debug("FormatEvent: failed to parse envelope",
			"event_type", ev.EventType, "session_id", ev.SessionID, "error", err)
		return ""
	}
	if len(envelope.Part) == 0 {
		return ""
	}

	// Parse the part directly into a LogEvent — no intermediate struct.
	var logEv LogEvent
	logEv.Timestamp = ev.Timestamp
	if err := json.Unmarshal(envelope.Part, &logEv.Part); err != nil {
		slog.Debug("FormatEvent: failed to parse part",
			"event_type", ev.EventType, "session_id", ev.SessionID, "error", err)
		return ""
	}

	ts := time.UnixMilli(ev.Timestamp).Format("15:04:05")

	switch logEv.Part.Type {
	case "text":
		logEv.Type = "text"
		return formatText(ts, logEv)
	case "tool":
		logEv.Type = "tool_use"
		return formatToolUse(ts, logEv)
	case "step-finish":
		logEv.Type = "step_finish"
		return formatStepFinish(ts, logEv)
	default:
		slog.Debug("FormatEvent: unhandled part type",
			"part_type", logEv.Part.Type, "session_id", ev.SessionID)
		return ""
	}
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
