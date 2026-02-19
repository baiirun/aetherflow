package daemon

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
// The log path is derived server-side from the spawn ID and the daemon's log
// directory — this matches the pool pattern (logFilePath) and prevents callers
// from pointing at arbitrary files.
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

	// Derive log path server-side from spawn ID, matching pool agent pattern.
	logPath := filepath.Join(d.config.LogDir, filepath.Base(params.SpawnID)+".jsonl")

	if err := d.spawns.Register(SpawnEntry{
		SpawnID:   params.SpawnID,
		PID:       params.PID,
		Prompt:    prompt,
		LogPath:   logPath,
		SpawnTime: time.Now(),
	}); err != nil {
		return &Response{Success: false, Error: err.Error()}
	}

	d.log.Info("spawn registered",
		"spawn_id", params.SpawnID,
		"pid", params.PID,
		"log_path", logPath,
	)

	// Session ID is captured when the session.created plugin event arrives
	// at the daemon — see event_rpc.go claimSession.

	return &Response{Success: true}
}

// SpawnDeregisterParams are the parameters for the spawn.deregister RPC method.
type SpawnDeregisterParams struct {
	SpawnID string `json:"spawn_id"`
}

// handleSpawnDeregister removes a spawned agent from the registry.
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

	entry := d.spawns.Get(params.SpawnID)
	d.spawns.Deregister(params.SpawnID)
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

	d.log.Info("spawn deregistered", "spawn_id", params.SpawnID)

	return &Response{Success: true}
}
