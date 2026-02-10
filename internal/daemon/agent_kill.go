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
// The agent's PID is validated against the pool, then SIGTERM is sent.
// The existing reap() goroutine handles cleanup after the process exits.
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

	// Find the agent by ID in the pool
	d.pool.mu.RLock()
	var agent *Agent
	for _, a := range d.pool.agents {
		if a.ID == agentID {
			agent = a
			break
		}
	}
	d.pool.mu.RUnlock()

	if agent == nil {
		return &Response{Success: false, Error: fmt.Sprintf("agent not found: %s", params.AgentName)}
	}

	// Send SIGTERM to the agent's process
	if err := syscallKill(agent.PID, syscall.SIGTERM); err != nil {
		return &Response{Success: false, Error: fmt.Sprintf("failed to send SIGTERM to PID %d: %v", agent.PID, err)}
	}

	d.log.Info("agent killed via RPC",
		"agent_id", agent.ID,
		"task_id", agent.TaskID,
		"pid", agent.PID,
	)

	result, err := json.Marshal(AgentKillResult{
		AgentName: params.AgentName,
		PID:       agent.PID,
	})
	if err != nil {
		return &Response{Success: false, Error: fmt.Sprintf("marshal error: %v", err)}
	}

	return &Response{Success: true, Result: result}
}
