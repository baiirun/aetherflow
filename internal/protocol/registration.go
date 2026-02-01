package protocol

import (
	"time"
)

// RegistrationResponse is returned by the daemon after registration.
type RegistrationResponse struct {
	AgentID AgentID `json:"agent_id"`
}

// AgentState represents the current state of an agent.
type AgentState string

const (
	StateIdle           AgentState = "idle"             // No active task
	StateActive         AgentState = "active"           // Working on a task
	StateQuestion       AgentState = "question"         // Waiting for clarification
	StateBlocked        AgentState = "blocked"          // Blocked on external dependency
	StateReadyForReview AgentState = "ready_for_review" // Work complete, awaiting review
	StateDone           AgentState = "done"             // Task completed
)

// AgentInfo represents the state of a registered agent.
type AgentInfo struct {
	ID           AgentID    `json:"id"`
	State        AgentState `json:"state"`
	RegisteredAt int64      `json:"registered_at"` // Unix milliseconds
	CurrentTask  string     `json:"current_task,omitempty"`
}

// IsExpired returns true if the agent hasn't been seen recently.
func (a *AgentInfo) IsExpired(timeout time.Duration) bool {
	return time.Since(time.UnixMilli(a.RegisteredAt)) > timeout
}
