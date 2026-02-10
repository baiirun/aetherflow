package daemon

import (
	"encoding/json"
	"fmt"
	"syscall"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

// syscallKill is the function used to send signals to processes.
// Exposed as a package variable for testing.
var syscallKill = syscall.Kill

// AgentKillParams are the parameters for the agent.kill RPC.
type AgentKillParams struct {
	AgentName string `json:"agent_name"`
}

// AgentKillResult is the response for the agent.kill RPC.
type AgentKillResult struct {
	AgentName string `json:"agent_name"`
	PID       int    `json:"pid"`
}

// handleAgentKill sends SIGTERM to the specified agent.
//
// The agent's PID is validated against the pool, then SIGTERM is sent.
// The existing reap() goroutine handles cleanup after the process exits.
//
// Race condition note: There's an acceptable TOCTOU gap between releasing
// the pool lock and sending the signal. If the agent exits naturally in
// this window, syscall.Kill returns ESRCH which is logged and returned to
// the caller. This window is small (microseconds) and the error is explicit,
// so the race is acceptable. The alternative (holding the lock during signal
// delivery) could block the pool for unpredictable durations if the kernel
// is slow to deliver signals.
func (d *Daemon) handleAgentKill(rawParams json.RawMessage) *Response {
	if d.pool == nil {
		return &Response{Success: false, Error: "no pool configured"}
	}

	var params AgentKillParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("invalid params: %v", err)}
		}
	}
	if params.AgentName == "" {
		return &Response{Success: false, Error: "agent_name is required"}
	}

	agentID := protocol.NewAgentIDFrom(params.AgentName)

	// Find the agent by ID in the pool and capture its immutable fields.
	d.pool.mu.RLock()
	var agent *Agent
	for _, a := range d.pool.agents {
		if a.ID == agentID {
			agent = a
			break
		}
	}

	if agent == nil {
		d.pool.mu.RUnlock()
		d.log.Warn("kill attempt on non-existent agent",
			"agent_name", params.AgentName,
		)
		return &Response{Success: false, Error: fmt.Sprintf("agent not found: %s", params.AgentName)}
	}

	// Capture fields while holding lock to minimize TOCTOU window.
	pid := agent.PID
	taskID := agent.TaskID
	agentIDCopy := agent.ID
	state := agent.State
	d.pool.mu.RUnlock()

	// Invariant checks: PID must be positive and agent must be running.
	// PID 0 kills the calling process group, negative PIDs have special
	// meanings in Unix (kill process groups), so we must validate.
	if pid <= 0 {
		d.log.Error("invalid PID in agent struct",
			"agent_id", agentIDCopy,
			"pid", pid,
		)
		return &Response{Success: false, Error: fmt.Sprintf("invalid agent PID: %d", pid)}
	}
	if state != AgentRunning {
		d.log.Warn("kill attempt on non-running agent",
			"agent_id", agentIDCopy,
			"state", state,
		)
		return &Response{Success: false, Error: fmt.Sprintf("agent is not running (state: %s)", state)}
	}

	// Send SIGTERM to the agent's process.
	// May fail with ESRCH if the process exited between lock release and here.
	if err := syscallKill(pid, syscall.SIGTERM); err != nil {
		d.log.Error("failed to send SIGTERM",
			"agent_id", agentIDCopy,
			"task_id", taskID,
			"pid", pid,
			"error", err,
		)
		// Provide more specific error message for common cases
		if err == syscall.ESRCH {
			return &Response{Success: false, Error: fmt.Sprintf("agent %s (PID %d) already exited", params.AgentName, pid)}
		}
		return &Response{Success: false, Error: fmt.Sprintf("failed to send SIGTERM to PID %d: %v", pid, err)}
	}

	d.log.Info("SIGTERM sent to agent",
		"agent_id", agentIDCopy,
		"task_id", taskID,
		"pid", pid,
	)

	result, err := json.Marshal(AgentKillResult{
		AgentName: params.AgentName,
		PID:       pid,
	})
	if err != nil {
		return &Response{Success: false, Error: fmt.Sprintf("marshal error: %v", err)}
	}

	return &Response{Success: true, Result: result}
}
