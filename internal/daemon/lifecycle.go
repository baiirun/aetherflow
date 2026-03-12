package daemon

import (
	"sort"
	"time"

	"github.com/baiirun/aetherflow/internal/protocol"
)

func activeWorkSnapshot(pool *Pool, spawns *SpawnRegistry) (count int, ids []string) {
	sessionSet := make(map[string]struct{})

	if pool != nil {
		for _, agent := range pool.Status() {
			if agent.State != AgentRunning {
				continue
			}
			count++
			if agent.SessionID == "" {
				continue
			}
			sessionSet[agent.SessionID] = struct{}{}
		}
	}

	if spawns != nil {
		for _, spawn := range spawns.List() {
			if spawn.State != SpawnRunning {
				continue
			}
			count++
			if spawn.SessionID == "" {
				continue
			}
			sessionSet[spawn.SessionID] = struct{}{}
		}
	}

	ids = make([]string, 0, len(sessionSet))
	for id := range sessionSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return count, ids
}

func (d *Daemon) lifecycleStatus() protocol.DaemonLifecycleStatus {
	d.lifeMu.RLock()
	status := d.life
	d.lifeMu.RUnlock()

	_, ids := activeWorkSnapshot(d.pool, d.spawns)
	status.ActiveSessionCount = len(ids)
	status.ActiveSessionIDs = ids
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now()
	}
	return status
}

func (d *Daemon) setLifecycleState(state protocol.LifecycleState, lastErr string) {
	d.lifeMu.Lock()
	d.life.State = state
	d.life.SocketPath = d.config.SocketPath
	d.life.Project = d.config.Project
	d.life.ServerURL = d.config.ServerURL
	d.life.SpawnPolicy = string(d.config.SpawnPolicy.Normalized())
	d.life.LastError = lastErr
	d.life.UpdatedAt = time.Now()
	d.lifeMu.Unlock()
}
