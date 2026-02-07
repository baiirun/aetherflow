package daemon

import (
	"encoding/json"
	"fmt"
)

// LogsPathParams are the parameters for the logs.path RPC method.
type LogsPathParams struct {
	AgentName string `json:"agent_name"`
}

// LogsPathResult is the response for the logs.path RPC method.
type LogsPathResult struct {
	Path string `json:"path"`
}

// handleLogsPath returns the JSONL log file path for a running agent.
// The CLI uses this path to tail the file directly (no streaming through the socket).
func (d *Daemon) handleLogsPath(rawParams json.RawMessage) *Response {
	var params LogsPathParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("invalid params: %v", err)}
		}
	}
	if params.AgentName == "" {
		return &Response{Success: false, Error: "agent_name is required"}
	}

	if d.pool == nil {
		return &Response{Success: false, Error: "no pool configured"}
	}

	// Find the agent by name to get its task ID.
	agents := d.pool.Status()
	var taskID string
	for _, a := range agents {
		if string(a.ID) == params.AgentName {
			taskID = a.TaskID
			break
		}
	}
	if taskID == "" {
		d.log.Warn("logs.path: agent not found", "agent", params.AgentName)
		return &Response{Success: false, Error: fmt.Sprintf("agent %q not found in pool", params.AgentName)}
	}

	path := logFilePath(d.config.LogDir, taskID)

	result, err := json.Marshal(LogsPathResult{Path: path})
	if err != nil {
		return &Response{Success: false, Error: fmt.Sprintf("marshal error: %v", err)}
	}

	d.log.Info("logs.path", "agent", params.AgentName, "path", path)

	return &Response{Success: true, Result: result}
}
