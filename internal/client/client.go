// Package client provides a client for communicating with aetherd.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/baiirun/aetherflow/internal/protocol"
)

// Client communicates with the aetherd daemon over HTTP.
type Client struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
}

// New creates a new client targeting the given daemon URL.
// If daemonURL is empty, the default daemon URL is used.
func New(daemonURL string) *Client {
	if daemonURL == "" {
		daemonURL = protocol.DefaultDaemonURL
	}
	return &Client{
		baseURL:   daemonURL,
		authToken: loadAuthToken(daemonURL),
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

// ShutdownRefusedError preserves the daemon-owned refusal outcome so callers
// can distinguish it from transport or protocol failures.
type ShutdownRefusedError struct {
	Result protocol.StopDaemonResult
}

func (e *ShutdownRefusedError) Error() string {
	return e.Result.Message
}

// doGet makes a GET request and decodes the result.
func (c *Client) doGet(path string, result any) error {
	req, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to aetherd: %w (is aetherd running?)", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return c.decodeResponse(resp, result)
}

// doPost makes a POST request with a JSON body and decodes the result.
func (c *Client) doPost(path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := c.newRequest(http.MethodPost, path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to aetherd: %w (is aetherd running?)", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return c.decodeResponse(resp, result)
}

// doDelete makes a DELETE request and decodes the result.
func (c *Client) doDelete(path string, result any) error {
	req, err := c.newRequest(http.MethodDelete, path, nil)
	if err != nil {
		return err
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
	if resp.StatusCode >= 400 {
		// Try to surface a structured error from the response body.
		var apiResp Response
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err == nil && apiResp.Error != "" {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiResp.Error)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

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

func (c *Client) newRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if c.authToken == "" {
		c.authToken = loadAuthToken(c.baseURL)
	}
	if c.authToken != "" {
		req.Header.Set("X-Aetherflow-Token", c.authToken)
	}
	return req, nil
}

func loadAuthToken(daemonURL string) string {
	path, err := authTokenPath(daemonURL)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func authTokenPath(daemonURL string) (string, error) {
	parsed, err := url.Parse(daemonURL)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		host = "127.0.0.1"
	}
	host = strings.NewReplacer(":", "_", "[", "", "]", "").Replace(host)
	port := parsed.Port()
	if port == "" {
		return "", fmt.Errorf("daemon url missing port")
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "aetherflow", "auth", fmt.Sprintf("%s_%s.token", host, port)), nil
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
	SpawnID         string    `json:"spawn_id"`
	PID             int       `json:"pid"`
	SessionID       string    `json:"session_id,omitempty"`
	State           string    `json:"state"`
	LifecycleState  string    `json:"lifecycle_state,omitempty"`
	LastActivityAt  time.Time `json:"last_activity_at,omitempty"`
	AttentionNeeded bool      `json:"attention_needed,omitempty"`
	Prompt          string    `json:"prompt"`
	SpawnTime       time.Time `json:"spawn_time"`
	ExitedAt        time.Time `json:"exited_at,omitempty"`
}

// AgentStatus is a single agent's enriched status.
type AgentStatus struct {
	ID              string    `json:"id"`
	TaskID          string    `json:"task_id"`
	Role            string    `json:"role"`
	PID             int       `json:"pid"`
	SpawnTime       time.Time `json:"spawn_time"`
	TaskTitle       string    `json:"task_title"`
	LastLog         string    `json:"last_log,omitempty"`
	SessionID       string    `json:"session_id,omitempty"`
	State           string    `json:"state,omitempty"`
	LifecycleState  string    `json:"lifecycle_state,omitempty"`
	LastActivityAt  time.Time `json:"last_activity_at,omitempty"`
	AttentionNeeded bool      `json:"attention_needed,omitempty"`
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
	Session   SessionMetadata `json:"session"`
	ToolCalls []ToolCall      `json:"tool_calls"`
	Errors    []string        `json:"errors,omitempty"`
}

// SessionMetadata is the session routing and handoff metadata exposed by the daemon.
type SessionMetadata struct {
	ServerRef  string    `json:"server_ref,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	Directory  string    `json:"directory,omitempty"`
	Project    string    `json:"project,omitempty"`
	OriginType string    `json:"origin_type,omitempty"`
	WorkRef    string    `json:"work_ref,omitempty"`
	AgentID    string    `json:"agent_id,omitempty"`
	Status     string    `json:"status,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	Attachable bool      `json:"attachable"`
}

// StatusAgentParams are the parameters for the status.agent RPC.
type StatusAgentParams struct {
	AgentName string `json:"agent_name"`
	Limit     int    `json:"limit,omitempty"`
}

// StatusAgent returns detailed status for a single agent including tool call history.
func (c *Client) StatusAgent(agentName string, limit int) (*AgentDetail, error) {
	path := "/api/v1/status/agents/" + url.PathEscape(agentName)
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

// DaemonLifecycle returns daemon lifecycle status.
func (c *Client) DaemonLifecycle() (*protocol.DaemonLifecycleStatus, error) {
	var result protocol.DaemonLifecycleStatus
	if err := c.doGet("/api/v1/lifecycle", &result); err != nil {
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
	Lines     []string        `json:"lines,omitempty"`
	Events    []SessionEvent  `json:"events,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Session   SessionMetadata `json:"session"`
	LastTS    int64           `json:"last_ts"`
}

// SessionEvent is a raw event from the daemon event buffer.
type SessionEvent struct {
	EventType string          `json:"event_type"`
	SessionID string          `json:"session_id"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// EventsList returns events for an agent from the daemon's event buffer.
// When afterTimestamp is set, only events after that timestamp are returned.
func (c *Client) EventsList(agentName string, afterTimestamp int64) (*EventsListResult, error) {
	vals := url.Values{}
	vals.Set("agent_name", agentName)
	if afterTimestamp > 0 {
		vals.Set("after_timestamp", strconv.FormatInt(afterTimestamp, 10))
	}
	path := "/api/v1/events?" + vals.Encode()
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
	path := "/api/v1/spawns/" + url.PathEscape(spawnID)
	return c.doDelete(path, nil)
}

// Shutdown stops the daemon. When force is false and the daemon has active
// sessions, it returns a "refused" error with a human-readable message.
// Pass force=true to stop unconditionally.
func (c *Client) Shutdown(force bool) error {
	_, err := c.StopDaemon(force)
	return err
}

// StopDaemon stops the daemon and returns the daemon-owned outcome.
func (c *Client) StopDaemon(force bool) (*protocol.StopDaemonResult, error) {
	path := "/api/v1/shutdown"
	if force {
		path += "?force=true"
	}
	var result protocol.StopDaemonResult
	if err := c.doPost(path, nil, &result); err != nil {
		return nil, err
	}
	if result.Outcome == protocol.StopOutcomeRefused {
		return &result, &ShutdownRefusedError{Result: result}
	}
	return &result, nil
}
