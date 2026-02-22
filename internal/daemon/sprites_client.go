package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (s *SpritesClient) Create(ctx context.Context, req ProviderCreateRequest) (ProviderCreateResult, error) {
	if s.token == "" {
		return ProviderCreateResult{}, fmt.Errorf("sprites token is required")
	}
	name, err := sanitizeSpriteName(req.SpawnID)
	if err != nil {
		return ProviderCreateResult{}, fmt.Errorf("invalid spawn ID for sprite name: %w", err)
	}
	body, _ := json.Marshal(spritesCreateRequest{Name: name})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v1/sprites", bytes.NewReader(body))
	if err != nil {
		return ProviderCreateResult{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.token)
	httpReq.Header.Set("Content-Type", "application/json")
	if req.RequestID != "" {
		httpReq.Header.Set("Idempotency-Key", req.RequestID)
	}

	resp, err := s.http.Do(httpReq)
	if err != nil {
		return ProviderCreateResult{}, fmt.Errorf("create sprite: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return ProviderCreateResult{}, fmt.Errorf("create sprite failed: status %d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out spritesObject
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return ProviderCreateResult{}, fmt.Errorf("decode create response: %w", err)
	}

	if strings.TrimSpace(out.ID) == "" {
		return ProviderCreateResult{}, fmt.Errorf("create sprite response missing id")
	}
	if strings.TrimSpace(out.Name) == "" {
		return ProviderCreateResult{}, fmt.Errorf("create sprite response missing name")
	}
	if strings.TrimSpace(out.URL) == "" {
		return ProviderCreateResult{}, fmt.Errorf("create sprite response missing url")
	}

	return ProviderCreateResult{
		SandboxID:   out.Name,
		OperationID: out.ID,
		AttachRef:   out.URL,
	}, nil
}

// validSpriteName matches lowercase alphanumeric names with hyphens, 1-63 chars.
// This mirrors typical cloud resource naming constraints (DNS label-compatible).
var validSpriteName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func sanitizeSpriteName(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", "-")
	v = strings.ReplaceAll(v, " ", "-")
	if v == "" {
		return "", fmt.Errorf("sprite name is required but was empty")
	}
	if !validSpriteName.MatchString(v) {
		return "", fmt.Errorf("sprite name %q contains invalid characters (allowed: lowercase alphanumeric, hyphens, 1-63 chars)", v)
	}
	return v, nil
}
