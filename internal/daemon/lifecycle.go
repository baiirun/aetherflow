package daemon

import (
	"sort"
	"time"

	"github.com/baiirun/aetherflow/internal/protocol"
)

type LifecycleState = protocol.LifecycleState

const (
	LifecycleStateStarting = protocol.LifecycleStateStarting
	LifecycleStateRunning  = protocol.LifecycleStateRunning
	LifecycleStateStopping = protocol.LifecycleStateStopping
	LifecycleStateStopped  = protocol.LifecycleStateStopped
	LifecycleStateFailed   = protocol.LifecycleStateFailed
)

type LifecycleStatus = protocol.DaemonLifecycleStatus
type StopDaemonParams = protocol.StopDaemonParams
type StopOutcome = protocol.StopOutcome

const (
	StopOutcomeStopped = protocol.StopOutcomeStopped
	StopOutcomeRefused = protocol.StopOutcomeRefused
	StopOutcomeFailed  = protocol.StopOutcomeFailed
)

type StopDaemonResult = protocol.StopDaemonResult

func activeSessionSnapshot(pool *Pool, spawns *SpawnRegistry) (count int, ids []string) {
	sessionSet := make(map[string]struct{})

	if pool != nil {
		for _, agent := range pool.Status() {
			if agent.SessionID == "" {
				continue
			}
			sessionSet[agent.SessionID] = struct{}{}
		}
	}

	if spawns != nil {
		for _, spawn := range spawns.List() {
			if spawn.SessionID == "" || spawn.State == SpawnExited {
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
	return len(ids), ids
}

func (d *Daemon) lifecycleStatus() LifecycleStatus {
	d.lifeMu.RLock()
	status := d.life
	d.lifeMu.RUnlock()

	count, ids := activeSessionSnapshot(d.pool, d.spawns)
	status.ActiveSessionCount = count
	status.ActiveSessionIDs = ids
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now()
	}
	return status
}

func (d *Daemon) setLifecycleState(state LifecycleState, lastErr string) {
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
