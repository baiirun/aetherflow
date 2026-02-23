package cmd

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/baiirun/aetherflow/internal/daemon"
	"github.com/baiirun/aetherflow/internal/sessions"
)

func TestLoadOpencodeSessionIndex(t *testing.T) {
	original := runCommandOutput
	t.Cleanup(func() { runCommandOutput = original })

	runCommandOutput = func(name string, args ...string) ([]byte, error) {
		if name != "opencode" {
			t.Fatalf("name = %q, want opencode", name)
		}
		return []byte(`[
  {"id":"ses_1","title":"Fix race in daemon","directory":"/tmp/proj"},
  {"id":"ses_2","title":"","directory":"/tmp/other"}
]`), nil
	}

	idx := loadOpencodeSessionIndex()
	if len(idx) != 2 {
		t.Fatalf("len(index) = %d, want 2", len(idx))
	}
	if idx["ses_1"].Title != "Fix race in daemon" {
		t.Fatalf("index[ses_1].Title = %q", idx["ses_1"].Title)
	}
}

func TestLoadOpencodeSessionIndexError(t *testing.T) {
	original := runCommandOutput
	t.Cleanup(func() { runCommandOutput = original })

	runCommandOutput = func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("boom")
	}

	idx := loadOpencodeSessionIndex()
	if len(idx) != 0 {
		t.Fatalf("len(index) = %d, want 0", len(idx))
	}
}

func TestSessionWhatForRecord(t *testing.T) {
	tests := []struct {
		name     string
		rec      sessions.Record
		index    map[string]opencodeSessionSummary
		semantic map[string]string
		want     string
	}{
		{
			name: "prefers semantic objective",
			rec:  sessions.Record{SessionID: "ses_1", ServerRef: "http://127.0.0.1:4096", WorkRef: "ts-123"},
			semantic: map[string]string{
				recordKey("http://127.0.0.1:4096", "ses_1"): "Run regression tests and report failures",
			},
			index: map[string]opencodeSessionSummary{
				"ses_1": {ID: "ses_1", Title: "Session purpose text"},
			},
			want: "Run regression tests and report failures",
		},
		{
			name: "prefers opencode title",
			rec:  sessions.Record{SessionID: "ses_1", WorkRef: "ts-123"},
			index: map[string]opencodeSessionSummary{
				"ses_1": {ID: "ses_1", Title: "Session purpose text"},
			},
			want: "Session purpose text",
		},
		{
			name: "falls back to work ref",
			rec:  sessions.Record{SessionID: "ses_missing", WorkRef: "ts-123"},
			want: "ts-123",
		},
		{
			name: "falls back to directory basename",
			rec:  sessions.Record{SessionID: "ses_2"},
			index: map[string]opencodeSessionSummary{
				"ses_2": {ID: "ses_2", Directory: "/Users/dev/repo-name"},
			},
			want: "dir: repo-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sessionWhatForRecord(tt.rec, tt.index, tt.semantic)
			if got != tt.want {
				t.Fatalf("sessionWhatForRecord() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestObjectiveFromPromptText(t *testing.T) {
	text := `"# Spawn Agent

## Objective

Run go test ./... and then summarize failures.

## Output

Done
"`
	got := objectiveFromPromptText(text)
	want := "Run go test ./... and then summarize failures."
	if got != want {
		t.Fatalf("objectiveFromPromptText() = %q, want %q", got, want)
	}
}

func TestShouldEnrichSessionTitle(t *testing.T) {
	tests := []struct {
		title string
		want  bool
	}{
		{title: "Autonomous Spawn Agent: setup", want: true},
		{title: "Spawn-ice_fox objective", want: true},
		{title: "New session - 2026-02-17T04:16:43.591Z", want: true},
		{title: "Implement websocket retries", want: false},
	}
	for _, tt := range tests {
		if got := shouldEnrichSessionTitle(tt.title); got != tt.want {
			t.Fatalf("shouldEnrichSessionTitle(%q) = %v, want %v", tt.title, got, tt.want)
		}
	}
}

func TestRemoteSpawnStatusToSessionStatus(t *testing.T) {
	tests := []struct {
		state daemon.RemoteSpawnState
		want  sessions.Status
	}{
		{state: daemon.RemoteSpawnRequested, want: sessions.StatusPending},
		{state: daemon.RemoteSpawnSpawning, want: sessions.StatusPending},
		{state: daemon.RemoteSpawnUnknown, want: sessions.StatusPending},
		{state: daemon.RemoteSpawnRunning, want: sessions.StatusActive},
		{state: daemon.RemoteSpawnFailed, want: sessions.StatusInactive},
		{state: daemon.RemoteSpawnTerminated, want: sessions.StatusInactive},
	}
	for _, tc := range tests {
		got := remoteSpawnStatusToSessionStatus(tc.state)
		if got != tc.want {
			t.Fatalf("remoteSpawnStatusToSessionStatus(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestSessionWhatForEntry(t *testing.T) {
	tests := []struct {
		name  string
		entry sessionListEntry
		want  string
	}{
		{
			name: "session-backed entry delegates to record logic",
			entry: sessionListEntry{
				Record: sessions.Record{SessionID: "ses_1", WorkRef: "ts-123"},
			},
			want: "ts-123",
		},
		{
			name: "remote spawn with provider shows provider prefix",
			entry: sessionListEntry{
				Record:   sessions.Record{Origin: sessions.OriginSpawn},
				SpawnID:  "spawn-ghost_wolf-a3f2",
				Provider: "sprites",
			},
			want: "sprites: spawn-ghost_wolf-a3f2",
		},
		{
			name: "remote spawn without provider shows spawn_id",
			entry: sessionListEntry{
				Record:  sessions.Record{Origin: sessions.OriginSpawn},
				SpawnID: "spawn-ghost_wolf-a3f2",
			},
			want: "spawn-ghost_wolf-a3f2",
		},
		{
			name: "empty entry returns dash",
			entry: sessionListEntry{
				Record: sessions.Record{},
			},
			want: "-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sessionWhatForEntry(tt.entry, nil, nil)
			if got != tt.want {
				t.Fatalf("sessionWhatForEntry() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildSessionListEntries(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)
	earliest := now.Add(-2 * time.Hour)

	t.Run("merges remote spawns and deduplicates by session_id", func(t *testing.T) {
		recs := []sessions.Record{
			{ServerRef: "http://127.0.0.1:4096", SessionID: "ses_1", UpdatedAt: earlier},
		}
		remoteRecs := []daemon.RemoteSpawnRecord{
			// This one already has ses_1 — should be deduped.
			{SpawnID: "spawn-a", SessionID: "ses_1", ServerRef: "http://127.0.0.1:4096", State: daemon.RemoteSpawnRunning, UpdatedAt: now},
			// This one is new — should appear.
			{SpawnID: "spawn-b", SessionID: "", ServerRef: "", State: daemon.RemoteSpawnSpawning, CreatedAt: earliest},
		}

		entries := buildSessionListEntries(recs, remoteRecs, "")
		if len(entries) != 2 {
			t.Fatalf("len(entries) = %d, want 2", len(entries))
		}
		// First entry should be ses_1 (earlier > earliest).
		if entries[0].SessionID != "ses_1" {
			t.Fatalf("entries[0].SessionID = %q, want ses_1", entries[0].SessionID)
		}
		// Second entry should be spawn-b.
		if entries[1].SpawnID != "spawn-b" {
			t.Fatalf("entries[1].SpawnID = %q, want spawn-b", entries[1].SpawnID)
		}
	})

	t.Run("sorts by UpdatedAt descending with CreatedAt fallback", func(t *testing.T) {
		recs := []sessions.Record{
			{ServerRef: "http://a", SessionID: "old", UpdatedAt: earliest},
			{ServerRef: "http://b", SessionID: "new", UpdatedAt: now},
		}
		entries := buildSessionListEntries(recs, nil, "")
		if entries[0].SessionID != "new" {
			t.Fatalf("entries[0].SessionID = %q, want new (most recent)", entries[0].SessionID)
		}
		if entries[1].SessionID != "old" {
			t.Fatalf("entries[1].SessionID = %q, want old", entries[1].SessionID)
		}
	})

	t.Run("applies server filter to remote spawns", func(t *testing.T) {
		remoteRecs := []daemon.RemoteSpawnRecord{
			{SpawnID: "spawn-a", ServerRef: "http://match", State: daemon.RemoteSpawnRunning, UpdatedAt: now},
			{SpawnID: "spawn-b", ServerRef: "http://other", State: daemon.RemoteSpawnRunning, UpdatedAt: now},
			// Empty ServerRef is excluded when filter is active.
			{SpawnID: "spawn-c", ServerRef: "", State: daemon.RemoteSpawnSpawning, CreatedAt: now},
		}
		entries := buildSessionListEntries(nil, remoteRecs, "http://match")
		if len(entries) != 1 {
			t.Fatalf("len(entries) = %d, want 1", len(entries))
		}
		if entries[0].SpawnID != "spawn-a" {
			t.Fatalf("entries[0].SpawnID = %q, want spawn-a", entries[0].SpawnID)
		}
	})

	t.Run("applies server filter to session records", func(t *testing.T) {
		recs := []sessions.Record{
			{ServerRef: "http://match", SessionID: "ses_1", UpdatedAt: now},
			{ServerRef: "http://other", SessionID: "ses_2", UpdatedAt: now},
		}
		entries := buildSessionListEntries(recs, nil, "http://match")
		if len(entries) != 1 {
			t.Fatalf("len(entries) = %d, want 1", len(entries))
		}
		if entries[0].SessionID != "ses_1" {
			t.Fatalf("entries[0].SessionID = %q, want ses_1", entries[0].SessionID)
		}
	})

	t.Run("maps remote spawn status correctly", func(t *testing.T) {
		remoteRecs := []daemon.RemoteSpawnRecord{
			{SpawnID: "spawn-a", State: daemon.RemoteSpawnSpawning, CreatedAt: now},
		}
		entries := buildSessionListEntries(nil, remoteRecs, "")
		if entries[0].Status != sessions.StatusPending {
			t.Fatalf("Status = %q, want %q", entries[0].Status, sessions.StatusPending)
		}
	})
}

// --- Regression tests: attach pending/error JSON contracts ---

func TestAttachPendingResultJSONContract(t *testing.T) {
	t.Parallel()

	result := attachPendingResult{
		Success:           false,
		Code:              "SESSION_NOT_READY",
		State:             "spawning",
		SpawnID:           "spawn-ghost_wolf-a3f2",
		SessionID:         "",
		RetryAfterSeconds: 5,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Required fields.
	if m["success"] != false {
		t.Errorf("success = %v, want false", m["success"])
	}
	if m["code"] != "SESSION_NOT_READY" {
		t.Errorf("code = %v, want %q", m["code"], "SESSION_NOT_READY")
	}
	if m["state"] != "spawning" {
		t.Errorf("state = %v, want %q", m["state"], "spawning")
	}
	if m["spawn_id"] != "spawn-ghost_wolf-a3f2" {
		t.Errorf("spawn_id = %v, want %q", m["spawn_id"], "spawn-ghost_wolf-a3f2")
	}
	if m["retry_after_seconds"] != float64(5) {
		t.Errorf("retry_after_seconds = %v, want 5", m["retry_after_seconds"])
	}

	// session_id should be omitted when empty.
	if _, ok := m["session_id"]; ok {
		t.Error("session_id should be omitted when empty (omitempty)")
	}
}

func TestAttachPendingResultWithSessionID(t *testing.T) {
	t.Parallel()

	result := attachPendingResult{
		Success:           false,
		Code:              "SESSION_NOT_READY",
		State:             "running",
		SpawnID:           "spawn-test",
		SessionID:         "ses_abc123",
		RetryAfterSeconds: 5,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if m["session_id"] != "ses_abc123" {
		t.Errorf("session_id = %v, want %q", m["session_id"], "ses_abc123")
	}
}

func TestAttachErrorResultJSONContract(t *testing.T) {
	t.Parallel()

	result := attachErrorResult{
		Success: false,
		Code:    "SESSION_NOT_AVAILABLE",
		State:   "failed",
		SpawnID: "spawn-ghost_wolf-a3f2",
		Error:   "spawn spawn-ghost_wolf-a3f2 is failed (see remote_spawns.json for details)",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if m["success"] != false {
		t.Errorf("success = %v, want false", m["success"])
	}
	if m["code"] != "SESSION_NOT_AVAILABLE" {
		t.Errorf("code = %v, want %q", m["code"], "SESSION_NOT_AVAILABLE")
	}
	if m["state"] != "failed" {
		t.Errorf("state = %v, want %q", m["state"], "failed")
	}
	if m["spawn_id"] != "spawn-ghost_wolf-a3f2" {
		t.Errorf("spawn_id = %v, want %q", m["spawn_id"], "spawn-ghost_wolf-a3f2")
	}
	if m["error"] == nil || m["error"] == "" {
		t.Error("error field should be populated")
	}
}

func TestAttachErrorResultOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	// When rec is nil, state and spawn_id should be omitted.
	result := attachErrorResult{
		Success: false,
		Code:    "REMOTE_SPAWN_STORE_ERROR",
		Error:   "store read failed",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	optional := []string{"state", "spawn_id"}
	for _, key := range optional {
		if _, ok := m[key]; ok {
			t.Errorf("%s should be omitted when empty (omitempty)", key)
		}
	}
}

// --- Regression tests: remote spawn state → session status mapping ---

func TestRemoteSpawnStatusToSessionStatusAllStates(t *testing.T) {
	t.Parallel()

	// Verify every known state maps correctly.
	tests := []struct {
		state daemon.RemoteSpawnState
		want  sessions.Status
	}{
		{daemon.RemoteSpawnRequested, sessions.StatusPending},
		{daemon.RemoteSpawnSpawning, sessions.StatusPending},
		{daemon.RemoteSpawnUnknown, sessions.StatusPending},
		{daemon.RemoteSpawnRunning, sessions.StatusActive},
		{daemon.RemoteSpawnFailed, sessions.StatusInactive},
		{daemon.RemoteSpawnTerminated, sessions.StatusInactive},
	}

	for _, tt := range tests {
		got := remoteSpawnStatusToSessionStatus(tt.state)
		if got != tt.want {
			t.Errorf("remoteSpawnStatusToSessionStatus(%q) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestRemoteSpawnStatusToSessionStatusUnknownState(t *testing.T) {
	t.Parallel()

	// Unknown/future states should default to pending (safe default).
	got := remoteSpawnStatusToSessionStatus(daemon.RemoteSpawnState("some-future-state"))
	if got != sessions.StatusPending {
		t.Errorf("unrecognized state mapped to %q, want %q (safe default)", got, sessions.StatusPending)
	}
}

// --- Regression tests: session list entry merge with local sessions ---

func TestBuildSessionListEntriesLocalOnly(t *testing.T) {
	t.Parallel()

	now := time.Now()
	recs := []sessions.Record{
		{ServerRef: "http://127.0.0.1:4096", SessionID: "ses_1", Status: sessions.StatusActive, UpdatedAt: now},
		{ServerRef: "http://127.0.0.1:4096", SessionID: "ses_2", Status: sessions.StatusActive, UpdatedAt: now.Add(-time.Minute)},
	}

	entries := buildSessionListEntries(recs, nil, "")
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	// Should be sorted by UpdatedAt descending.
	if entries[0].SessionID != "ses_1" {
		t.Errorf("entries[0].SessionID = %q, want ses_1 (most recent)", entries[0].SessionID)
	}
	// SpawnID should be empty for local sessions.
	if entries[0].SpawnID != "" {
		t.Errorf("entries[0].SpawnID = %q, want empty for local session", entries[0].SpawnID)
	}
}

func TestBuildSessionListEntriesRemoteOnly(t *testing.T) {
	t.Parallel()

	now := time.Now()
	remoteRecs := []daemon.RemoteSpawnRecord{
		{SpawnID: "spawn-a", Provider: "sprites", State: daemon.RemoteSpawnRunning, ServerRef: "https://test.sprites.app", SessionID: "ses_remote_1", UpdatedAt: now},
		{SpawnID: "spawn-b", Provider: "sprites", State: daemon.RemoteSpawnSpawning, CreatedAt: now.Add(-time.Minute)},
	}

	entries := buildSessionListEntries(nil, remoteRecs, "")
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].SpawnID != "spawn-a" {
		t.Errorf("entries[0].SpawnID = %q, want spawn-a (most recent)", entries[0].SpawnID)
	}
	if entries[0].Provider != "sprites" {
		t.Errorf("entries[0].Provider = %q, want sprites", entries[0].Provider)
	}
	if entries[1].SpawnID != "spawn-b" {
		t.Errorf("entries[1].SpawnID = %q, want spawn-b", entries[1].SpawnID)
	}
	if entries[1].Status != sessions.StatusPending {
		t.Errorf("entries[1].Status = %q, want pending (spawning state)", entries[1].Status)
	}
}

func TestSessionListEntryJSONContract(t *testing.T) {
	// Verify the JSON shape includes spawn_id and provider when set.
	entry := sessionListEntry{
		Record: sessions.Record{
			ServerRef: "https://test.sprites.app",
			Origin:    sessions.OriginSpawn,
			Status:    sessions.StatusPending,
		},
		SpawnID:  "spawn-ghost_wolf-a3f2",
		Provider: "sprites",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if m["spawn_id"] != "spawn-ghost_wolf-a3f2" {
		t.Fatalf("spawn_id = %v, want %q", m["spawn_id"], "spawn-ghost_wolf-a3f2")
	}
	if m["provider"] != "sprites" {
		t.Fatalf("provider = %v, want %q", m["provider"], "sprites")
	}
}
