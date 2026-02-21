package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	defaultSpritesAPIBaseURL = "https://api.sprites.dev"
)

// SpritesClient is a minimal API client implementing the Provider boundary.
type SpritesClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewSpritesClient creates a Sprites API client.
func NewSpritesClient(token string) *SpritesClient {
	return &SpritesClient{
		baseURL: defaultSpritesAPIBaseURL,
		token:   strings.TrimSpace(token),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

type spritesCreateRequest struct {
	Name string `json:"name"`
}

type spritesObject struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	URL    string `json:"url"`
}

func (s *SpritesClient) Create(ctx context.Context, req ProviderCreateRequest) (ProviderCreateResult, error) {
	if s.token == "" {
		return ProviderCreateResult{}, fmt.Errorf("sprites token is required")
	}
	name := sanitizeSpriteName(req.SpawnID)
	body, _ := json.Marshal(spritesCreateRequest{Name: name})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v1/sprites", bytes.NewReader(body))
	if err != nil {
		return ProviderCreateResult{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(httpReq)
	if err != nil {
		return ProviderCreateResult{}, fmt.Errorf("create sprite: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return ProviderCreateResult{}, fmt.Errorf("create sprite failed: status %d", resp.StatusCode)
	}

	var out spritesObject
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ProviderCreateResult{}, fmt.Errorf("decode create response: %w", err)
	}

	return ProviderCreateResult{
		SandboxID:   name,
		OperationID: out.ID,
		AttachRef:   out.URL,
	}, nil
}

func (s *SpritesClient) GetStatus(ctx context.Context, sandboxID string) (ProviderStatusResult, error) {
	if s.token == "" {
		return ProviderStatusResult{}, fmt.Errorf("sprites token is required")
	}
	name := sanitizeSpriteName(sandboxID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/v1/sprites/"+name, nil)
	if err != nil {
		return ProviderStatusResult{}, fmt.Errorf("status request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.http.Do(httpReq)
	if err != nil {
		return ProviderStatusResult{}, fmt.Errorf("get sprite status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return ProviderStatusResult{Status: ProviderRuntimeTerminated}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return ProviderStatusResult{}, fmt.Errorf("get sprite status failed: status %d", resp.StatusCode)
	}

	var out spritesObject
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ProviderStatusResult{}, fmt.Errorf("decode status response: %w", err)
	}

	return ProviderStatusResult{
		Status:    mapSpritesStatus(out.Status),
		AttachRef: out.URL,
	}, nil
}

func (s *SpritesClient) Terminate(ctx context.Context, sandboxID string) error {
	if s.token == "" {
		return fmt.Errorf("sprites token is required")
	}
	name := sanitizeSpriteName(sandboxID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.baseURL+"/v1/sprites/"+name, nil)
	if err != nil {
		return fmt.Errorf("terminate request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("terminate sprite: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("terminate sprite failed: status %d", resp.StatusCode)
}

func mapSpritesStatus(in string) ProviderRuntimeStatus {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "running":
		return ProviderRuntimeRunning
	case "warm":
		return ProviderRuntimeWarm
	case "cold":
		return ProviderRuntimeCold
	default:
		return ProviderRuntimeUnknown
	}
}

func sanitizeSpriteName(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", "-")
	v = strings.ReplaceAll(v, " ", "-")
	if v == "" {
		return "sprite"
	}
	return v
}
