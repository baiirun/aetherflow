// Package client provides a client for communicating with aetherd.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/baiirun/aetherflow/internal/protocol"
)

// Client communicates with the aetherd daemon over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new client targeting the given daemon URL.
// If daemonURL is empty, the default daemon URL is used.
func New(daemonURL string) *Client {
	if daemonURL == "" {
		daemonURL = protocol.DefaultDaemonURL
	}
	return &Client{
		baseURL: daemonURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Response is the JSON response envelope from the daemon API.
type Response struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// doGet makes a GET request and decodes the result.
func (c *Client) doGet(path string, result any) error {
	url := c.baseURL + path
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("failed to connect to aetherd: %w (is aetherd running?)", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return c.decodeResponse(resp, result)
}

// doPost makes a POST request with a JSON body and decodes the result.
func (c *Client) doPost(path string, body any, result any) error {
	url := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	resp, err := c.httpClient.Post(url, "application/json", bodyReader)
	if err != nil {
		return fmt.Errorf("failed to connect to aetherd: %w (is aetherd running?)", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return c.decodeResponse(resp, result)
}

// doDelete makes a DELETE request and decodes the result.
func (c *Client) doDelete(path string, result any) error {
	url := c.baseURL + path
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to aetherd: %w (is aetherd running?)", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return c.decodeResponse(resp, result)
}

// decodeResponse reads and decodes the JSON response envelope.
func (c *Client) decodeResponse(resp *http.Response, result any) error {
	var apiResp Response
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("%s", apiResp.Error)
	}

	if result != nil && len(apiResp.Result) > 0 {
		if err := json.Unmarshal(apiResp.Result, result); err != nil {
			return fmt.Errorf("failed to parse result: %w", err)
		}
	}

	return nil
}

// FullStatus is the enriched swarm status returned by the status.full RPC.
type FullStatus struct {
	PoolSize    int           `json:"pool_size"`
	PoolMode    string        `json:"pool_mode"`
	Project     string        `json:"project"`
	SpawnPolicy string        `json:"spawn_policy"`
	Agents      []AgentStatus `json:"agents"`
	Spawns      []SpawnStatus `json:"spawns,omitempty"`
	Queue       []Task        `json:"queue"`
	Errors      []string      `json:"errors,omitempty"`
}

const (
	SpawnPolicyAuto   = "auto"
	SpawnPolicyManual = "manual"

	// SpawnState constants for display code. These mirror the daemon's
	// SpawnState type but are plain strings — the client package doesn't
	// import daemon types. The JSON wire format is the boundary.
	SpawnStateRunning = "running"
	SpawnStateExited  = "exited"
)

// NormalizedSpawnPolicy returns the effective spawn policy for status display.
func (s *FullStatus) NormalizedSpawnPolicy() string {
	if s.SpawnPolicy == "" {
		return SpawnPolicyManual
	}
	return s.SpawnPolicy
}

// IsManualSpawnPolicy reports whether status is in manual spawn policy mode.
func (s *FullStatus) IsManualSpawnPolicy() bool {
	return s.NormalizedSpawnPolicy() == SpawnPolicyManual
}

// SpawnStatus is the status of a spawned agent registered with the daemon.
type SpawnStatus struct {
	SpawnID   string    `json:"spawn_id"`
	PID       int       `json:"pid"`
	SessionID string    `json:"session_id,omitempty"`
	State     string    `json:"state"`
	Prompt    string    `json:"prompt"`
	SpawnTime time.Time `json:"spawn_time"`
	ExitedAt  time.Time `json:"exited_at,omitempty"`
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

// ToolCall is a single tool invocation from the agent's event stream.
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
	path := fmt.Sprintf("/api/v1/status/agents/%s", url.PathEscape(agentName))
	if limit > 0 {
		path = fmt.Sprintf("%s?limit=%d", path, limit)
	}
	var result AgentDetail
	if err := c.doGet(path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// StatusFull returns the enriched swarm status with task metadata from prog.
func (c *Client) StatusFull() (*FullStatus, error) {
	var result FullStatus
	if err := c.doGet("/api/v1/status", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// EventsListParams are the parameters for the events.list RPC.
type EventsListParams struct {
	AgentName      string `json:"agent_name"`
	AfterTimestamp int64  `json:"after_timestamp,omitempty"`
	Raw            bool   `json:"raw,omitempty"`
}

// EventsListResult is the response for the events.list RPC.
type EventsListResult struct {
	Lines     []string `json:"lines,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	LastTS    int64    `json:"last_ts"`
}

// EventsList returns events for an agent from the daemon's event buffer.
// When afterTimestamp is set, only events after that timestamp are returned.
func (c *Client) EventsList(agentName string, afterTimestamp int64) (*EventsListResult, error) {
	path := fmt.Sprintf("/api/v1/events?agent_name=%s", url.QueryEscape(agentName))
	if afterTimestamp > 0 {
		path = fmt.Sprintf("%s&after_timestamp=%d", path, afterTimestamp)
	}
	var result EventsListResult
	if err := c.doGet(path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PoolModeResult is the response for pool control RPCs.
type PoolModeResult struct {
	Mode    string `json:"mode"`
	Running int    `json:"running"`
}

// PoolDrain transitions the pool to draining mode.
func (c *Client) PoolDrain() (*PoolModeResult, error) {
	var result PoolModeResult
	if err := c.doPost("/api/v1/pool/drain", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PoolPause transitions the pool to paused mode.
func (c *Client) PoolPause() (*PoolModeResult, error) {
	var result PoolModeResult
	if err := c.doPost("/api/v1/pool/pause", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PoolResume transitions the pool back to active mode.
func (c *Client) PoolResume() (*PoolModeResult, error) {
	var result PoolModeResult
	if err := c.doPost("/api/v1/pool/resume", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SpawnRegisterParams are the parameters for the spawn.register RPC.
type SpawnRegisterParams struct {
	SpawnID string `json:"spawn_id"`
	PID     int    `json:"pid"`
	Prompt  string `json:"prompt"`
}

// SpawnRegister registers a spawned agent with the daemon for observability.
// This is best-effort — if the daemon isn't running, the error is returned
// and the caller can proceed without registration.
func (c *Client) SpawnRegister(params SpawnRegisterParams) error {
	return c.doPost("/api/v1/spawns", params, nil)
}

// SpawnDeregister marks a spawned agent as exited in the daemon's registry.
func (c *Client) SpawnDeregister(spawnID string) error {
	path := fmt.Sprintf("/api/v1/spawns/%s", url.PathEscape(spawnID))
	return c.doDelete(path, nil)
}

// Shutdown stops the daemon.
func (c *Client) Shutdown() error {
	return c.doPost("/api/v1/shutdown", nil, nil)
}
