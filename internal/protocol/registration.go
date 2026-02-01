package protocol

import (
	"time"
)

// RegistrationRequest is sent by an agent to register with the daemon.
type RegistrationRequest struct {
	// Name is an optional human-readable name for the agent.
	Name string `json:"name,omitempty"`

	// Labels are optional tags for the agent (e.g., "frontend", "rust", "senior").
	Labels []string `json:"labels,omitempty"`

	// Capacity is the maximum number of tasks the agent can have queued.
	// Default is 5 if not specified.
	Capacity int `json:"capacity,omitempty"`

	// HeartbeatInterval is how often the agent will send heartbeats.
	// Default is 30 seconds if not specified.
	HeartbeatIntervalSecs int `json:"heartbeat_interval_secs,omitempty"`
}

// RegistrationResponse is returned by the daemon after registration.
type RegistrationResponse struct {
	// AgentID is the daemon-assigned identifier for this agent.
	AgentID AgentID `json:"agent_id"`

	// Accepted indicates whether the registration was accepted.
	Accepted bool `json:"accepted"`

	// Reason explains why registration was rejected (if Accepted is false).
	Reason string `json:"reason,omitempty"`

	// HeartbeatIntervalSecs is the confirmed heartbeat interval.
	HeartbeatIntervalSecs int `json:"heartbeat_interval_secs"`

	// LeaseExpiresAt is when the agent's registration expires if no heartbeat.
	// Unix milliseconds.
	LeaseExpiresAt int64 `json:"lease_expires_at"`
}

// Heartbeat is sent periodically by agents to maintain their registration.
type Heartbeat struct {
	AgentID AgentID    `json:"agent_id"`
	TS      int64      `json:"ts"` // Unix milliseconds
	State   AgentState `json:"state"`

	// QueueDepth is the current number of messages in the agent's inbox.
	QueueDepth int `json:"queue_depth"`

	// CurrentTask is the task ID the agent is currently working on (if any).
	CurrentTask string `json:"current_task,omitempty"`
}

// HeartbeatResponse is returned by the daemon after a heartbeat.
type HeartbeatResponse struct {
	// Acknowledged indicates the heartbeat was received.
	Acknowledged bool `json:"acknowledged"`

	// LeaseExpiresAt is the updated lease expiration time.
	LeaseExpiresAt int64 `json:"lease_expires_at"`

	// PendingMessages is the number of messages waiting in the agent's inbox.
	PendingMessages int `json:"pending_messages"`
}

// AgentState represents the current state of an agent.
type AgentState string

const (
	StateIdle           AgentState = "idle"             // No active task
	StateQueued         AgentState = "queued"           // Task assigned, not yet started
	StateActive         AgentState = "active"           // Working on a task
	StateQuestion       AgentState = "question"         // Waiting for clarification
	StateBlocked        AgentState = "blocked"          // Blocked on external dependency
	StateReadyForReview AgentState = "ready_for_review" // Work complete, awaiting review
	StateReview         AgentState = "review"           // Under review
	StateDone           AgentState = "done"             // Task completed
	StateAbandoned      AgentState = "abandoned"        // Task abandoned
)

// AgentInfo represents the full state of a registered agent.
type AgentInfo struct {
	ID       AgentID    `json:"id"`
	Name     string     `json:"name,omitempty"`
	Labels   []string   `json:"labels,omitempty"`
	Capacity int        `json:"capacity"`
	State    AgentState `json:"state"`

	// Registration info
	RegisteredAt   int64 `json:"registered_at"`    // Unix milliseconds
	LastHeartbeat  int64 `json:"last_heartbeat"`   // Unix milliseconds
	LeaseExpiresAt int64 `json:"lease_expires_at"` // Unix milliseconds

	// Queue info
	QueueDepth  int    `json:"queue_depth"`
	CurrentTask string `json:"current_task,omitempty"`
}

// IsExpired returns true if the agent's lease has expired.
func (a *AgentInfo) IsExpired() bool {
	return time.Now().UnixMilli() > a.LeaseExpiresAt
}

// UnregisterRequest is sent to remove an agent from the system.
type UnregisterRequest struct {
	AgentID AgentID `json:"agent_id"`

	// Reason explains why the agent is unregistering.
	Reason string `json:"reason,omitempty"`
}

// UnregisterResponse is returned after unregistration.
type UnregisterResponse struct {
	// Success indicates whether unregistration succeeded.
	Success bool `json:"success"`

	// PendingMessages is the number of messages that were requeued.
	PendingMessages int `json:"pending_messages"`
}
