package daemon

import (
	"encoding/json"
	"fmt"
)

// SessionEventParams are the parameters for the session.event RPC method.
// These arrive from the opencode plugin via the daemon's Unix socket.
type SessionEventParams struct {
	AgentID   string          `json:"agent_id"`
	EventType string          `json:"event_type"`
	SessionID string          `json:"session_id,omitempty"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// handleSessionEvent receives an event from the opencode plugin and stores
// it in the per-agent event buffer. This is the daemon's end of the plugin
// event pipeline — the plugin pushes all server events here fire-and-forget.
func (d *Daemon) handleSessionEvent(rawParams json.RawMessage) *Response {
	var params SessionEventParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("invalid params: %v", err)}
		}
	}
	if params.AgentID == "" {
		return &Response{Success: false, Error: "agent_id is required"}
	}
	if params.EventType == "" {
		return &Response{Success: false, Error: "event_type is required"}
	}

	d.events.Push(SessionEvent{
		AgentID:   params.AgentID,
		EventType: params.EventType,
		SessionID: params.SessionID,
		Timestamp: params.Timestamp,
		Data:      params.Data,
	})

	d.log.Debug("session.event",
		"agent_id", params.AgentID,
		"event_type", params.EventType,
		"session_id", params.SessionID,
	)

	return &Response{Success: true}
}
