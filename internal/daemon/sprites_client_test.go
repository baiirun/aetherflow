package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Integration tests: SpritesClient ---

func TestSpritesClientCreateHappyPath(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method and path.
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/sprites" {
			t.Errorf("path = %q, want /v1/sprites", r.URL.Path)
		}

		// Verify auth header.
		if got := r.Header.Get("Authorization"); got != "Bearer test-token-123" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-token-123")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		// Verify request body.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading body: %v", err)
		}
		var req spritesCreateRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("parsing body: %v", err)
		}
		if req.Name != "spawn-ghost-wolf-a3f2" {
			t.Errorf("request name = %q, want %q", req.Name, "spawn-ghost-wolf-a3f2")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(spritesObject{
			ID:   "spr_abc123",
			Name: "spawn-ghost-wolf-a3f2",
			URL:  "https://spawn-ghost-wolf-a3f2.sprites.app",
		})
	}))
	defer ts.Close()

	client := NewSpritesClient("test-token-123")
	client.baseURL = ts.URL

	result, err := client.Create(context.Background(), ProviderCreateRequest{
		SpawnID:   "spawn-ghost_wolf-a3f2",
		RequestID: "req-001",
		Project:   "testproject",
		Prompt:    "fix the auth bug",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.SandboxID != "spawn-ghost-wolf-a3f2" {
		t.Errorf("SandboxID = %q, want %q", result.SandboxID, "spawn-ghost-wolf-a3f2")
	}
	if result.OperationID != "spr_abc123" {
		t.Errorf("OperationID = %q, want %q", result.OperationID, "spr_abc123")
	}
	if result.AttachRef != "https://spawn-ghost-wolf-a3f2.sprites.app" {
		t.Errorf("AttachRef = %q, want %q", result.AttachRef, "https://spawn-ghost-wolf-a3f2.sprites.app")
	}
}

func TestSpritesClientCreateIdempotencyKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requestID string
		wantKey   string
	}{
		{
			name:      "sends idempotency key when request ID set",
			requestID: "req-idempotent-001",
			wantKey:   "req-idempotent-001",
		},
		{
			name:      "omits idempotency key when request ID empty",
			requestID: "",
			wantKey:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.Header.Get("Idempotency-Key")
				if got != tt.wantKey {
					t.Errorf("Idempotency-Key = %q, want %q", got, tt.wantKey)
				}

				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(spritesObject{
					ID:   "spr_1",
					Name: "spawn-test",
					URL:  "https://test.sprites.app",
				})
			}))
			defer ts.Close()

			client := NewSpritesClient("tok")
			client.baseURL = ts.URL

			_, err := client.Create(context.Background(), ProviderCreateRequest{
				SpawnID:   "spawn-test",
				RequestID: tt.requestID,
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
		})
	}
}

func TestSpritesClientCreateAuthFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    string
	}{
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"invalid token"}`,
			wantErr:    "status 401",
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			body:       `{"error":"insufficient permissions"}`,
			wantErr:    "status 403",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := NewSpritesClient("bad-token")
			client.baseURL = ts.URL

			_, err := client.Create(context.Background(), ProviderCreateRequest{
				SpawnID:   "spawn-test",
				RequestID: "req-1",
			})
			if err == nil {
				t.Fatal("expected error for auth failure, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSpritesClientCreateEmptyToken(t *testing.T) {
	t.Parallel()

	client := NewSpritesClient("")

	_, err := client.Create(context.Background(), ProviderCreateRequest{
		SpawnID:   "spawn-test",
		RequestID: "req-1",
	})
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if !strings.Contains(err.Error(), "token is required") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "token is required")
	}
}

func TestSpritesClientCreateServerError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    string
	}{
		{
			name:       "500 internal server error",
			statusCode: http.StatusInternalServerError,
			body:       "internal server error",
			wantErr:    "status 500",
		},
		{
			name:       "502 bad gateway",
			statusCode: http.StatusBadGateway,
			body:       "bad gateway",
			wantErr:    "status 502",
		},
		{
			name:       "429 rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":"rate limit exceeded"}`,
			wantErr:    "status 429",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := NewSpritesClient("valid-token")
			client.baseURL = ts.URL

			_, err := client.Create(context.Background(), ProviderCreateRequest{
				SpawnID:   "spawn-test",
				RequestID: "req-1",
			})
			if err == nil {
				t.Fatal("expected error for server error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSpritesClientCreateMissingResponseFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		resp    spritesObject
		wantErr string
	}{
		{
			name:    "missing id",
			resp:    spritesObject{ID: "", Name: "test", URL: "https://test.sprites.app"},
			wantErr: "missing id",
		},
		{
			name:    "missing name",
			resp:    spritesObject{ID: "spr_1", Name: "", URL: "https://test.sprites.app"},
			wantErr: "missing name",
		},
		{
			name:    "missing url",
			resp:    spritesObject{ID: "spr_1", Name: "test", URL: ""},
			wantErr: "missing url",
		},
		{
			name:    "whitespace-only id",
			resp:    spritesObject{ID: "   ", Name: "test", URL: "https://test.sprites.app"},
			wantErr: "missing id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(tt.resp)
			}))
			defer ts.Close()

			client := NewSpritesClient("valid-token")
			client.baseURL = ts.URL

			_, err := client.Create(context.Background(), ProviderCreateRequest{
				SpawnID:   "spawn-test",
				RequestID: "req-1",
			})
			if err == nil {
				t.Fatal("expected error for missing field, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSpritesClientCreateTimeout(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block longer than client timeout.
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(spritesObject{
			ID: "spr_1", Name: "test", URL: "https://test.sprites.app",
		})
	}))
	defer ts.Close()

	client := NewSpritesClient("valid-token")
	client.baseURL = ts.URL
	// Override timeout to something short for tests.
	client.http.Timeout = 100 * time.Millisecond

	_, err := client.Create(context.Background(), ProviderCreateRequest{
		SpawnID:   "spawn-test",
		RequestID: "req-1",
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// The error should be a network/timeout error, not a status code error.
	if strings.Contains(err.Error(), "status") {
		t.Errorf("error should be a timeout, not status error: %v", err)
	}
}

func TestSpritesClientCreateContextCanceled(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	client := NewSpritesClient("valid-token")
	client.baseURL = ts.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := client.Create(ctx, ProviderCreateRequest{
		SpawnID:   "spawn-test",
		RequestID: "req-1",
	})
	if err == nil {
		t.Fatal("expected context canceled error, got nil")
	}
}

func TestSpritesClientCreateInvalidJSON(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer ts.Close()

	client := NewSpritesClient("valid-token")
	client.baseURL = ts.URL

	_, err := client.Create(context.Background(), ProviderCreateRequest{
		SpawnID:   "spawn-test",
		RequestID: "req-1",
	})
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "decode")
	}
}

func TestSpritesClientCreate200OK(t *testing.T) {
	// Some APIs return 200 instead of 201 — verify we accept both.
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(spritesObject{
			ID:   "spr_200",
			Name: "spawn-test",
			URL:  "https://test.sprites.app",
		})
	}))
	defer ts.Close()

	client := NewSpritesClient("valid-token")
	client.baseURL = ts.URL

	result, err := client.Create(context.Background(), ProviderCreateRequest{
		SpawnID:   "spawn-test",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.OperationID != "spr_200" {
		t.Errorf("OperationID = %q, want %q", result.OperationID, "spr_200")
	}
}

// --- Unit tests: sanitizeSpriteName ---

func TestSanitizeSpriteName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "simple lowercase",
			input: "spawn-test",
			want:  "spawn-test",
		},
		{
			name:  "underscores converted to hyphens",
			input: "spawn-ghost_wolf-a3f2",
			want:  "spawn-ghost-wolf-a3f2",
		},
		{
			name:  "uppercase converted to lowercase",
			input: "Spawn-Test",
			want:  "spawn-test",
		},
		{
			name:  "spaces converted to hyphens",
			input: "spawn test",
			want:  "spawn-test",
		},
		{
			name:  "leading/trailing whitespace trimmed",
			input: "  spawn-test  ",
			want:  "spawn-test",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "invalid characters",
			input:   "spawn@test!",
			wantErr: true,
		},
		{
			name:  "max length 63",
			input: strings.Repeat("a", 63),
			want:  strings.Repeat("a", 63),
		},
		{
			name:    "exceeds 63 chars",
			input:   strings.Repeat("a", 64),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := sanitizeSpriteName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("sanitizeSpriteName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- Integration test: remote spawn store + SpritesClient E2E flow ---

func TestRemoteSpawnE2EFlow(t *testing.T) {
	t.Parallel()

	// Simulate the complete spawn flow: store record → provider create → update record.
	dir := t.TempDir()
	store, err := OpenRemoteSpawnStore(dir)
	if err != nil {
		t.Fatalf("OpenRemoteSpawnStore() error = %v", err)
	}

	// Step 1: Mock sprites API.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(spritesObject{
			ID:   "spr_e2e_123",
			Name: "spawn-e2e-test",
			URL:  "https://spawn-e2e-test.sprites.app",
		})
	}))
	defer ts.Close()

	spawnID := "spawn-e2e-test"
	requestID := "req-e2e-001"

	// Step 2: Persist initial record (state=requested).
	rec := RemoteSpawnRecord{
		SpawnID:   spawnID,
		Provider:  "sprites",
		RequestID: requestID,
		State:     RemoteSpawnRequested,
	}
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert(requested) error = %v", err)
	}

	// Verify initial state.
	got, err := store.GetBySpawnID(spawnID)
	if err != nil {
		t.Fatalf("GetBySpawnID() error = %v", err)
	}
	if got.State != RemoteSpawnRequested {
		t.Fatalf("state = %q, want %q", got.State, RemoteSpawnRequested)
	}

	// Step 3: Call provider.
	client := NewSpritesClient("test-token")
	client.baseURL = ts.URL

	created, err := client.Create(context.Background(), ProviderCreateRequest{
		SpawnID:   spawnID,
		RequestID: requestID,
		Project:   "testproject",
		Prompt:    "fix the tests",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Step 4: Update record with provider result (state=spawning).
	rec.ProviderSandboxID = created.SandboxID
	rec.ProviderOperation = created.OperationID
	rec.ServerRef = created.AttachRef
	rec.State = RemoteSpawnSpawning
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert(spawning) error = %v", err)
	}

	// Verify updated state.
	got, err = store.GetBySpawnID(spawnID)
	if err != nil {
		t.Fatalf("GetBySpawnID() error = %v", err)
	}
	if got.State != RemoteSpawnSpawning {
		t.Fatalf("state = %q, want %q", got.State, RemoteSpawnSpawning)
	}
	if got.ProviderSandboxID != "spawn-e2e-test" {
		t.Errorf("ProviderSandboxID = %q, want %q", got.ProviderSandboxID, "spawn-e2e-test")
	}
	if got.ServerRef != "https://spawn-e2e-test.sprites.app" {
		t.Errorf("ServerRef = %q, want %q", got.ServerRef, "https://spawn-e2e-test.sprites.app")
	}

	// Step 5: Simulate session discovery (state=running with session_id).
	rec.State = RemoteSpawnRunning
	rec.SessionID = "ses_e2e_abc"
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert(running) error = %v", err)
	}

	got, err = store.GetBySpawnID(spawnID)
	if err != nil {
		t.Fatalf("GetBySpawnID() error = %v", err)
	}
	if got.State != RemoteSpawnRunning {
		t.Fatalf("state = %q, want %q", got.State, RemoteSpawnRunning)
	}
	if got.SessionID != "ses_e2e_abc" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "ses_e2e_abc")
	}

	// Step 6: Verify idempotency — same request_id should conflict.
	dup := RemoteSpawnRecord{
		SpawnID:   "spawn-duplicate",
		Provider:  "sprites",
		RequestID: requestID,
		State:     RemoteSpawnRequested,
	}
	dupErr := store.Upsert(dup)
	if !IsIdempotencyConflict(dupErr) {
		t.Fatalf("expected idempotency conflict for duplicate request_id, got: %v", dupErr)
	}

	// Idempotency lookup should return the original record.
	existing, err := store.GetByProviderRequest("sprites", requestID)
	if err != nil {
		t.Fatalf("GetByProviderRequest() error = %v", err)
	}
	if existing.SpawnID != spawnID {
		t.Errorf("existing.SpawnID = %q, want %q", existing.SpawnID, spawnID)
	}
}

func TestRemoteSpawnE2EProviderFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := OpenRemoteSpawnStore(dir)
	if err != nil {
		t.Fatalf("OpenRemoteSpawnStore() error = %v", err)
	}

	// Mock sprites API that returns 500.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("provider unavailable"))
	}))
	defer ts.Close()

	spawnID := "spawn-fail-test"

	rec := RemoteSpawnRecord{
		SpawnID:   spawnID,
		Provider:  "sprites",
		RequestID: "req-fail-001",
		State:     RemoteSpawnRequested,
	}
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert(requested) error = %v", err)
	}

	client := NewSpritesClient("test-token")
	client.baseURL = ts.URL

	_, createErr := client.Create(context.Background(), ProviderCreateRequest{
		SpawnID:   spawnID,
		RequestID: "req-fail-001",
	})
	if createErr == nil {
		t.Fatal("expected provider error, got nil")
	}

	// Simulate the spawn.go error handling: set state to failed.
	rec.State = RemoteSpawnFailed
	rec.LastError = createErr.Error()
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert(failed) error = %v", err)
	}

	got, err := store.GetBySpawnID(spawnID)
	if err != nil {
		t.Fatalf("GetBySpawnID() error = %v", err)
	}
	if got.State != RemoteSpawnFailed {
		t.Fatalf("state = %q, want %q", got.State, RemoteSpawnFailed)
	}
	if got.LastError == "" {
		t.Error("LastError should be set after provider failure")
	}
}

func TestRemoteSpawnE2EAmbiguousTimeout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := OpenRemoteSpawnStore(dir)
	if err != nil {
		t.Fatalf("OpenRemoteSpawnStore() error = %v", err)
	}

	// Mock sprites API that hangs (simulating timeout).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer ts.Close()

	spawnID := "spawn-timeout-test"

	// Step 1: Record starts at requested.
	rec := RemoteSpawnRecord{
		SpawnID:   spawnID,
		Provider:  "sprites",
		RequestID: "req-timeout-001",
		State:     RemoteSpawnRequested,
	}
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert(requested) error = %v", err)
	}

	// Step 2: Transition to spawning (provider call in progress).
	// This is what happens before the HTTP call — the state moves to
	// spawning to indicate the provider call was attempted.
	rec.State = RemoteSpawnSpawning
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert(spawning) error = %v", err)
	}

	// Step 3: Make the provider call, which times out.
	client := NewSpritesClient("test-token")
	client.baseURL = ts.URL
	client.http.Timeout = 100 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, createErr := client.Create(ctx, ProviderCreateRequest{
		SpawnID:   spawnID,
		RequestID: "req-timeout-001",
	})
	if createErr == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// Step 4: Ambiguous timeout → state=unknown (valid from spawning).
	rec.State = RemoteSpawnUnknown
	rec.LastError = createErr.Error()
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert(unknown) error = %v", err)
	}

	got, err := store.GetBySpawnID(spawnID)
	if err != nil {
		t.Fatalf("GetBySpawnID() error = %v", err)
	}
	if got.State != RemoteSpawnUnknown {
		t.Fatalf("state = %q, want %q (ambiguous timeout should be unknown)", got.State, RemoteSpawnUnknown)
	}
}
