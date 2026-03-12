package daemon

import (
	"time"

	"github.com/baiirun/aetherflow/internal/sessions"
)

// SessionMetadata is the daemon/client-facing session handle for detail and
// handoff flows. It carries the routing identity plus registry-backed context
// needed to render a stable session header and launch attach-based handoff.
type SessionMetadata struct {
	ServerRef  string    `json:"server_ref,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	Directory  string    `json:"directory,omitempty"`
	Project    string    `json:"project,omitempty"`
	OriginType string    `json:"origin_type,omitempty"`
	WorkRef    string    `json:"work_ref,omitempty"`
	AgentID    string    `json:"agent_id,omitempty"`
	Status     string    `json:"status,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	Attachable bool      `json:"attachable"`
}

type sessionMetadataFallback struct {
	serverRef string
	sessionID string
	project   string
	origin    sessions.OriginType
	workRef   string
	agentID   string
	status    string
	directory string
}

func buildSessionMetadata(sstore *sessions.Store, fallback sessionMetadataFallback) SessionMetadata {
	meta := SessionMetadata{
		ServerRef:  fallback.serverRef,
		SessionID:  fallback.sessionID,
		Directory:  fallback.directory,
		Project:    fallback.project,
		OriginType: string(fallback.origin),
		WorkRef:    fallback.workRef,
		AgentID:    fallback.agentID,
		Status:     fallback.status,
	}

	if rec, ok := loadSessionRecord(sstore, fallback.serverRef, fallback.sessionID); ok {
		meta.ServerRef = rec.ServerRef
		meta.SessionID = rec.SessionID
		meta.Directory = rec.Directory
		meta.Project = rec.Project
		meta.OriginType = string(rec.Origin)
		meta.WorkRef = rec.WorkRef
		meta.AgentID = rec.AgentID
		meta.Status = string(rec.Status)
		meta.CreatedAt = rec.CreatedAt
		meta.LastSeenAt = rec.LastSeenAt
		meta.UpdatedAt = rec.UpdatedAt
	}

	meta.Attachable = sessionAttachable(meta.ServerRef, meta.SessionID)
	return meta
}

func loadSessionRecord(sstore *sessions.Store, serverRef, sessionID string) (sessions.Record, bool) {
	if sstore == nil || serverRef == "" || sessionID == "" {
		return sessions.Record{}, false
	}
	recs, err := sstore.List()
	if err != nil {
		return sessions.Record{}, false
	}
	for _, rec := range recs {
		if rec.ServerRef == serverRef && rec.SessionID == sessionID {
			return rec, true
		}
	}
	return sessions.Record{}, false
}

func sessionAttachable(serverRef, sessionID string) bool {
	if sessionID == "" {
		return false
	}
	_, err := ValidateServerURLLocal(serverRef)
	return err == nil
}
