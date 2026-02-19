package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// opencodeClient is a minimal HTTP client for the opencode server REST API.
// It currently supports only the endpoints needed for event buffer backfill.
// The client is reusable for future API orchestration (POST /session, etc.).
type opencodeClient struct {
	baseURL    string
	httpClient *http.Client
}

// newOpencodeClient creates a client targeting the given server URL.
// The URL should be the same as Config.ServerURL (e.g. http://127.0.0.1:4096).
func newOpencodeClient(baseURL string) *opencodeClient {
	return &opencodeClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// apiMessage is a message returned by GET /session/:id/message.
// Each message contains parts (text, tool calls, step markers, etc.).
type apiMessage struct {
	ID    string            `json:"id"`
	Parts []json.RawMessage `json:"parts"` // raw part objects, parsed individually
}

// fetchSessionMessages fetches the full message tree for a session
// and returns parsed messages with raw part payloads.
func (c *opencodeClient) fetchSessionMessages(ctx context.Context, sessionID string) ([]apiMessage, error) {
	url := fmt.Sprintf("%s/session/%s/message", c.baseURL, sessionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching session messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET %s returned %d: %s", url, resp.StatusCode, string(body))
	}

	var messages []apiMessage
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, fmt.Errorf("decoding session messages: %w", err)
	}

	return messages, nil
}

// fetchSessionStatus fetches the status of all sessions from the server.
// Returns a map of session ID → status type (e.g. "busy", "idle").
func (c *opencodeClient) fetchSessionStatus(ctx context.Context) (map[string]string, error) {
	url := fmt.Sprintf("%s/session/status", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching session status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET %s returned %d: %s", url, resp.StatusCode, string(body))
	}

	// Response shape: {"ses_xxx": {"type": "idle"}, ...}
	var raw map[string]struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding session status: %w", err)
	}

	result := make(map[string]string, len(raw))
	for id, s := range raw {
		result[id] = s.Type
	}
	return result, nil
}
