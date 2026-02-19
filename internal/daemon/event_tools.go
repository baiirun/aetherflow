package daemon

import (
	"encoding/json"
	"time"
)

// eventPartEnvelope is the sparse parse target for a message.part.updated event's
// Data payload. Only the fields needed for tool call extraction are included.
type eventPartEnvelope struct {
	Part struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Tool  string `json:"tool"`
		State struct {
			Status string          `json:"status"`
			Input  json.RawMessage `json:"input"`
			Title  string          `json:"title"`
			Time   struct {
				Start int64 `json:"start"` // Unix millis
				End   int64 `json:"end"`   // Unix millis
			} `json:"time"`
		} `json:"state"`
	} `json:"part"`
}

// ToolCallsFromEvents extracts tool calls from session events.
// It scans message.part.updated events where part.type is "tool",
// keeping only the latest state per part ID (events arrive as a lifecycle:
// pending → running → completed). Returns up to limit most recent calls
// (0 means all), ordered by event timestamp.
func ToolCallsFromEvents(events []SessionEvent, limit int) []ToolCall {
	// Build a map of part ID → latest tool call, preserving insertion order.
	type entry struct {
		order int
		call  ToolCall
	}
	seen := make(map[string]*entry)
	nextOrder := 0

	for _, ev := range events {
		if ev.EventType != "message.part.updated" {
			continue
		}
		if len(ev.Data) == 0 {
			continue
		}

		var envelope eventPartEnvelope
		if err := json.Unmarshal(ev.Data, &envelope); err != nil {
			continue
		}

		if envelope.Part.Type != "tool" {
			continue
		}

		partID := envelope.Part.ID
		if partID == "" {
			// Fallback: use event timestamp as key (shouldn't happen in practice).
			partID = string(rune(nextOrder))
		}

		tc := ToolCall{
			Timestamp: time.UnixMilli(ev.Timestamp),
			Tool:      envelope.Part.Tool,
			Title:     envelope.Part.State.Title,
			Status:    envelope.Part.State.Status,
			Input:     extractKeyInput(envelope.Part.Tool, envelope.Part.State.Input),
		}

		if envelope.Part.State.Time.Start > 0 && envelope.Part.State.Time.End > 0 {
			tc.DurationMs = int(envelope.Part.State.Time.End - envelope.Part.State.Time.Start)
		}

		if existing, ok := seen[partID]; ok {
			// Update in place — later events have more complete state.
			existing.call = tc
		} else {
			seen[partID] = &entry{order: nextOrder, call: tc}
			nextOrder++
		}
	}

	// Collect in insertion order.
	calls := make([]ToolCall, 0, len(seen))
	ordered := make([]*entry, 0, len(seen))
	for _, e := range seen {
		ordered = append(ordered, e)
	}
	// Sort by insertion order to preserve chronological sequence.
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if ordered[j].order < ordered[i].order {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	for _, e := range ordered {
		calls = append(calls, e.call)
	}

	if limit > 0 && len(calls) > limit {
		calls = calls[len(calls)-limit:]
	}

	return calls
}
