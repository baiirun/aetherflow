package protocol

import "time"

// LifecycleState is the daemon lifecycle state exposed over RPC.
type LifecycleState string

const (
	LifecycleStateStarting LifecycleState = "starting"
	LifecycleStateRunning  LifecycleState = "running"
	LifecycleStateStopping LifecycleState = "stopping"
	LifecycleStateStopped  LifecycleState = "stopped"
	LifecycleStateFailed   LifecycleState = "failed"
)

// DaemonLifecycleStatus is the daemon lifecycle contract.
type DaemonLifecycleStatus struct {
	State              LifecycleState `json:"state"`
	DaemonURL          string         `json:"daemon_url,omitempty"`
	Project            string         `json:"project,omitempty"`
	ServerURL          string         `json:"server_url,omitempty"`
	SpawnPolicy        string         `json:"spawn_policy,omitempty"`
	ActiveSessionCount int            `json:"active_session_count,omitempty"`
	ActiveSessionIDs   []string       `json:"active_session_ids,omitempty"`
	LastError          string         `json:"last_error,omitempty"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// StopDaemonParams controls daemon stop behavior.
type StopDaemonParams struct {
	Force bool `json:"force,omitempty"`
}

// StopOutcome is the outcome of a stop request.
type StopOutcome string

const (
	StopOutcomeStopping StopOutcome = "stopping"
	StopOutcomeStopped  StopOutcome = "stopped"
	StopOutcomeRefused  StopOutcome = "refused"
)

// StopDaemonResult captures daemon-owned stop outcomes.
type StopDaemonResult struct {
	Outcome StopOutcome           `json:"outcome"`
	Status  DaemonLifecycleStatus `json:"status"`
	Message string                `json:"message,omitempty"`
}
