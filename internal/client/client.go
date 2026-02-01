// Package client provides a client for communicating with aetherd.
package client

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

const DefaultSocketPath = "/tmp/aetherd.sock"

// Client communicates with the aetherd daemon.
type Client struct {
	socketPath string
}

// New creates a new client.
func New(socketPath string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath
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

// Status returns the daemon status.
func (c *Client) Status() (map[string]any, error) {
	var result map[string]any
	if err := c.call("status", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Register registers a new agent with the daemon.
func (c *Client) Register() (*protocol.RegistrationResponse, error) {
	var resp protocol.RegistrationResponse
	if err := c.call("register", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Unregister removes an agent from the daemon.
func (c *Client) Unregister(agentID protocol.AgentID) error {
	params := map[string]any{"agent_id": agentID}
	return c.call("unregister", params, nil)
}

// ListAgents returns all registered agents.
func (c *Client) ListAgents() ([]*protocol.AgentInfo, error) {
	var agents []*protocol.AgentInfo
	if err := c.call("list_agents", nil, &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

// Shutdown stops the daemon.
func (c *Client) Shutdown() error {
	return c.call("shutdown", nil, nil)
}
