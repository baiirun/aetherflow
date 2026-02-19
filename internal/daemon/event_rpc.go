package daemon

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/baiirun/aetherflow/internal/sessions"
)

// SessionEventParams are the parameters for the session.event RPC method.
// These arrive from the opencode plugin via the daemon's Unix socket.
type SessionEventParams struct {
	EventType string          `json:"event_type"`
	SessionID string          `json:"session_id"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// handleSessionEvent receives an event from the opencode plugin and stores
// it in the per-session event buffer. This is the daemon's end of the plugin
// event pipeline — the plugin pushes all server events here fire-and-forget.
//
// On session.created events, the handler also correlates the session ID to
// a pool agent or spawn entry that hasn't been assigned a session yet.
func (d *Daemon) handleSessionEvent(rawParams json.RawMessage) *Response {
	var params SessionEventParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("invalid params: %v", err)}
		}
	}
	if params.SessionID == "" {
		return &Response{Success: false, Error: "session_id is required"}
	}
	if params.EventType == "" {
		return &Response{Success: false, Error: "event_type is required"}
	}

	d.events.Push(SessionEvent{
		EventType: params.EventType,
		SessionID: params.SessionID,
		Timestamp: params.Timestamp,
		Data:      params.Data,
	})

	d.log.Debug("session.event",
		"event_type", params.EventType,
		"session_id", params.SessionID,
	)

	if params.EventType == "session.created" {
		d.claimSession(params.SessionID)
	}

	return &Response{Success: true}
}

// claimSession correlates a newly-created session ID to a pool agent or
// spawn entry that hasn't been assigned a session yet. This replaces the
// old JSONL-polling approach (captureSessionFromLog / captureSpawnSession).
//
// If exactly one unclaimed candidate exists across pool and spawns, the
// session ID is assigned to it and persisted in the session registry.
// If zero or multiple candidates exist, the event is logged but no
// assignment happens — the common case is one agent at a time.
func (d *Daemon) claimSession(sessionID string) {
	type candidate struct {
		kind    string // "pool" or "spawn"
		agentID string // pool agent name or spawn ID
	}
	var candidates []candidate

	// Check pool for agents without a session ID.
	if d.pool != nil {
		for _, a := range d.pool.Status() {
			if a.SessionID == "" && a.State == AgentRunning {
				candidates = append(candidates, candidate{kind: "pool", agentID: string(a.ID)})
			}
		}
	}

	// Check spawn registry for entries without a session ID.
	for _, s := range d.spawns.List() {
		if s.SessionID == "" {
			candidates = append(candidates, candidate{kind: "spawn", agentID: s.SpawnID})
		}
	}

	if len(candidates) == 0 {
		d.log.Debug("session.created: no unclaimed agents to assign",
			"session_id", sessionID,
		)
		return
	}

	if len(candidates) > 1 {
		d.log.Warn("session.created: multiple unclaimed agents, skipping auto-assign",
			"session_id", sessionID,
			"candidates", len(candidates),
		)
		return
	}

	c := candidates[0]
	d.log.Info("session claimed",
		"session_id", sessionID,
		"kind", c.kind,
		"agent_id", c.agentID,
	)

	switch c.kind {
	case "pool":
		if d.pool != nil {
			d.pool.SetSessionID(c.agentID, sessionID)
		}
		if d.sstore != nil {
			rec := sessions.Record{
				ServerRef:  d.config.ServerURL,
				SessionID:  sessionID,
				Project:    d.config.Project,
				Origin:     sessions.OriginPool,
				WorkRef:    d.pool.TaskIDForAgent(c.agentID),
				AgentID:    c.agentID,
				Status:     sessions.StatusActive,
				LastSeenAt: time.Now(),
			}
			if err := d.sstore.Upsert(rec); err != nil {
				d.log.Warn("failed to persist pool session record",
					"session_id", sessionID,
					"agent_id", c.agentID,
					"error", err,
				)
			}
		}

	case "spawn":
		d.spawns.SetSessionID(c.agentID, sessionID)
		if d.sstore != nil {
			rec := sessions.Record{
				ServerRef:  d.config.ServerURL,
				SessionID:  sessionID,
				Project:    d.config.Project,
				Origin:     sessions.OriginSpawn,
				WorkRef:    c.agentID,
				Status:     sessions.StatusActive,
				LastSeenAt: time.Now(),
			}
			if err := d.sstore.Upsert(rec); err != nil {
				d.log.Warn("failed to persist spawn session record",
					"session_id", sessionID,
					"spawn_id", c.agentID,
					"error", err,
				)
			}
		}
	}
}
