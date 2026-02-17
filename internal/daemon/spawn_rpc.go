package daemon

import (
	"context"
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

	go d.captureSpawnSession(logPath, params.SpawnID)

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

	d.spawns.Deregister(params.SpawnID)
	if d.sstore != nil {
		_ = d.sstore.SetStatusByWorkRef(sessions.OriginSpawn, params.SpawnID, sessions.StatusIdle)
	}

	d.log.Info("spawn deregistered", "spawn_id", params.SpawnID)

	return &Response{Success: true}
}

func (d *Daemon) captureSpawnSession(path, spawnID string) {
	if d.sstore == nil {
		return
	}

	const maxAttempts = 40
	for i := 0; i < maxAttempts; i++ {
		sid, err := ParseSessionID(context.Background(), path)
		if err == nil && sid != "" {
			rec := sessions.Record{
				ServerRef: d.config.ServerURL,
				SessionID: sid,
				Project:   d.config.Project,
				Origin:    sessions.OriginSpawn,
				WorkRef:   spawnID,
				Status:    sessions.StatusActive,
			}
			if err := d.sstore.Upsert(rec); err != nil {
				d.log.Warn("failed to persist spawn session record", "spawn_id", spawnID, "error", err)
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}
