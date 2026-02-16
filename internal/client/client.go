// Package client provides a client for communicating with aetherd.
package client

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/baiirun/aetherflow/internal/protocol"
)

// Client communicates with the aetherd daemon.
type Client struct {
	socketPath string
}

// New creates a new client.
func New(socketPath string) *Client {
	if socketPath == "" {
		socketPath = protocol.DefaultSocketPath
	}
	return &Client{socketPath: socketPath}
}

// Request is the JSON-RPC style request envelope.
type Request struct {
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

// Response is the JSON-RPC style response envelope.
type Response struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

func (c *Client) call(method string, params any, result any) error {
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to aetherd: %w (is aetherd running?)", err)
	}
	defer conn.Close()

	req := Request{Method: method, Params: params}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("failed to parse result: %w", err)
		}
	}

	return nil
}

// FullStatus is the enriched swarm status returned by the status.full RPC.
type FullStatus struct {
	PoolSize int           `json:"pool_size"`
	PoolMode string        `json:"pool_mode"`
	Project  string        `json:"project"`
	Agents   []AgentStatus `json:"agents"`
	Queue    []Task        `json:"queue"`
	Errors   []string      `json:"errors,omitempty"`
}

// AgentStatus is a single agent's enriched status.
type AgentStatus struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	Role      string    `json:"role"`
	PID       int       `json:"pid"`
	SpawnTime time.Time `json:"spawn_time"`
	TaskTitle string    `json:"task_title"`
	LastLog   string    `json:"last_log,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
}

// Task is a pending task from the queue.
type Task struct {
	ID       string `json:"id"`
	Priority int    `json:"priority"`
	Title    string `json:"title"`
}

// ToolCall is a single tool invocation from the agent's JSONL log.
type ToolCall struct {
	Timestamp  time.Time `json:"timestamp"`
	Tool       string    `json:"tool"`
	Title      string    `json:"title,omitempty"`
	Input      string    `json:"input"`
	Status     string    `json:"status"`
	DurationMs int       `json:"duration_ms,omitempty"`
}

// AgentDetail is the detailed view of a single agent with tool call history.
type AgentDetail struct {
	AgentStatus
	ToolCalls []ToolCall `json:"tool_calls"`
	Errors    []string   `json:"errors,omitempty"`
}

// StatusAgentParams are the parameters for the status.agent RPC.
type StatusAgentParams struct {
	AgentName string `json:"agent_name"`
	Limit     int    `json:"limit,omitempty"`
}

// StatusAgent returns detailed status for a single agent including tool call history.
func (c *Client) StatusAgent(agentName string, limit int) (*AgentDetail, error) {
	params := StatusAgentParams{AgentName: agentName, Limit: limit}
	var result AgentDetail
	if err := c.call("status.agent", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// StatusFull returns the enriched swarm status with task metadata from prog.
func (c *Client) StatusFull() (*FullStatus, error) {
	var result FullStatus
	if err := c.call("status.full", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// LogsPathParams are the parameters for the logs.path RPC.
type LogsPathParams struct {
	AgentName string `json:"agent_name"`
}

// LogsPathResult is the response for the logs.path RPC.
type LogsPathResult struct {
	Path string `json:"path"`
}

// LogsPath returns the JSONL log file path for a running agent.
func (c *Client) LogsPath(agentName string) (string, error) {
	params := LogsPathParams{AgentName: agentName}
	var result LogsPathResult
	if err := c.call("logs.path", params, &result); err != nil {
		return "", err
	}
	return result.Path, nil
}

// PoolModeResult is the response for pool control RPCs.
type PoolModeResult struct {
	Mode    string `json:"mode"`
	Running int    `json:"running"`
}

// PoolDrain transitions the pool to draining mode.
func (c *Client) PoolDrain() (*PoolModeResult, error) {
	var result PoolModeResult
	if err := c.call("pool.drain", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PoolPause transitions the pool to paused mode.
func (c *Client) PoolPause() (*PoolModeResult, error) {
	var result PoolModeResult
	if err := c.call("pool.pause", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PoolResume transitions the pool back to active mode.
func (c *Client) PoolResume() (*PoolModeResult, error) {
	var result PoolModeResult
	if err := c.call("pool.resume", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Shutdown stops the daemon.
func (c *Client) Shutdown() error {
	return c.call("shutdown", nil, nil)
}
