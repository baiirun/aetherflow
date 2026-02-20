package daemon

import (
	"encoding/json"
	"time"
)

// ToolCall is a single tool invocation extracted from an agent's event stream.
type ToolCall struct {
	Timestamp  time.Time `json:"timestamp"`
	Tool       string    `json:"tool"`
	Title      string    `json:"title,omitempty"`
	Input      string    `json:"input"`
	Status     string    `json:"status"`
	DurationMs int       `json:"duration_ms,omitempty"`
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
