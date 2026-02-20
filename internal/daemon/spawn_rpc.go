package daemon

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/baiirun/aetherflow/internal/sessions"
)

const (
	// maxSpawnIDLen caps spawn ID length to prevent log-line bloat and
	// path-length issues (spawn ID flows into branch names and log paths).
	maxSpawnIDLen = 128
)

// SpawnRegisterParams are the parameters for the spawn.register RPC method.
type SpawnRegisterParams struct {
	SpawnID string `json:"spawn_id"`
	PID     int    `json:"pid"`
	Prompt  string `json:"prompt"`
}

// handleSpawnRegister registers a spawned agent with the daemon for observability.
func (d *Daemon) handleSpawnRegister(rawParams json.RawMessage) *Response {
	var params SpawnRegisterParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("invalid params: %v", err)}
		}
	}
	if params.SpawnID == "" {
		return &Response{Success: false, Error: "spawn_id is required"}
	}
	if len(params.SpawnID) > maxSpawnIDLen {
		return &Response{Success: false, Error: fmt.Sprintf("spawn_id too long (%d > %d)", len(params.SpawnID), maxSpawnIDLen)}
	}
	if params.PID <= 0 {
		return &Response{Success: false, Error: "pid must be positive"}
	}

	// Truncate prompt to cap memory usage — only used for display.
	prompt := params.Prompt
	if len(prompt) > maxSpawnPromptLen {
		prompt = prompt[:maxSpawnPromptLen]
	}

	if err := d.spawns.Register(SpawnEntry{
		SpawnID:   params.SpawnID,
		PID:       params.PID,
		State:     SpawnRunning,
		Prompt:    prompt,
		SpawnTime: time.Now(),
	}); err != nil {
		return &Response{Success: false, Error: err.Error()}
	}

	d.log.Info("spawn registered",
		"spawn_id", params.SpawnID,
		"pid", params.PID,
	)

	// Session ID is captured when the session.created plugin event arrives
	// at the daemon — see event_rpc.go claimSession.

	return &Response{Success: true}
}

// SpawnDeregisterParams are the parameters for the spawn.deregister RPC method.
type SpawnDeregisterParams struct {
	SpawnID string `json:"spawn_id"`
}

// handleSpawnDeregister marks a spawned agent as exited in the registry.
// The entry is kept (preserving the agent→session mapping for af status)
// until the periodic sweep removes it after exitedSpawnTTL.
func (d *Daemon) handleSpawnDeregister(rawParams json.RawMessage) *Response {
	var params SpawnDeregisterParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("invalid params: %v", err)}
		}
	}
	if params.SpawnID == "" {
		return &Response{Success: false, Error: "spawn_id is required"}
	}

	if !d.spawns.MarkExited(params.SpawnID) {
		d.log.Warn("spawn deregister: entry not found or already exited", "spawn_id", params.SpawnID)
	} else {
		d.log.Info("spawn exited", "spawn_id", params.SpawnID)
	}

	// Update session status regardless — the session store may have a record
	// even if the spawn registry entry was already cleaned up.
	entry := d.spawns.Get(params.SpawnID)
	if d.sstore != nil {
		if entry != nil && entry.SessionID != "" {
			if _, err := d.sstore.SetStatusBySession(d.config.ServerURL, entry.SessionID, sessions.StatusIdle); err != nil {
				d.log.Warn("failed to update spawn session status by key", "spawn_id", params.SpawnID, "session_id", entry.SessionID, "status", sessions.StatusIdle, "error", err)
			}
		}
		if _, err := d.sstore.SetStatusByWorkRef(sessions.OriginSpawn, params.SpawnID, sessions.StatusIdle); err != nil {
			d.log.Warn("failed to update spawn session status", "spawn_id", params.SpawnID, "status", sessions.StatusIdle, "error", err)
		}
	}

	return &Response{Success: true}
}
