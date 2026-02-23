package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// statusRunner creates a CommandRunner for status tests.
// showResponses maps task IDs to `prog show --json` output.
// readyOutput is the raw output for `prog ready`.
func statusRunner(showResponses map[string]string, readyOutput string) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "show" {
			taskID := args[1]
			resp, ok := showResponses[taskID]
			if !ok {
				return nil, fmt.Errorf("task not found: %s", taskID)
			}
			return []byte(resp), nil
		}
		if len(args) >= 1 && args[0] == "ready" {
			return []byte(readyOutput), nil
		}
		return nil, fmt.Errorf("unexpected command: %s %v", name, args)
	}
}

// statusPool creates a pool with agents pre-loaded for status tests.
// No goroutines are started — agents are manually inserted.
func statusPool(t *testing.T, agents map[string]*Agent) *Pool {
	t.Helper()
	cfg := Config{
		Project:  "testproject",
		PoolSize: 3,
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, nil, nil, slog.Default())
	pool.mu.Lock()
	for k, v := range agents {
		pool.agents[k] = v
	}
	pool.mu.Unlock()
	return pool
}

func TestBuildFullStatus(t *testing.T) {
	now := time.Now()

	agents := map[string]*Agent{
		"ts-abc": {
			ID:        "blur_knife",
			TaskID:    "ts-abc",
			Role:      RoleWorker,
			PID:       1234,
			SpawnTime: now.Add(-12 * time.Minute),
			State:     AgentRunning,
		},
		"ts-def": {
			ID:        "sharp_proxy",
			TaskID:    "ts-def",
			Role:      RoleWorker,
			PID:       5678,
			SpawnTime: now.Add(-3 * time.Minute),
			State:     AgentRunning,
		},
	}

	showResponses := map[string]string{
		"ts-abc": `{"title":"Fix spawn() race condition","logs":[{"message":"Fixed spawn(), 3 tests remain","created_at":"2026-02-07T10:00:00Z"}]}`,
		"ts-def": `{"title":"Refactor config loading","logs":[{"message":"All tests passing, running review","created_at":"2026-02-07T10:05:00Z"}]}`,
	}

	readyOutput := "ID           PRI  TITLE\nts-ghi    1    Fix auth token expiry\nts-jkl    2    Refactor config loading\n"

	pool := statusPool(t, agents)
	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyAuto}
	runner := statusRunner(showResponses, readyOutput)

	status := BuildFullStatus(context.Background(), StatusSources{Pool: pool}, cfg, runner)

	if status.PoolSize != 3 {
		t.Errorf("PoolSize = %d, want 3", status.PoolSize)
	}
	if status.Project != "testproject" {
		t.Errorf("Project = %q, want %q", status.Project, "testproject")
	}
	if status.SpawnPolicy != SpawnPolicyAuto {
		t.Errorf("SpawnPolicy = %q, want %q", status.SpawnPolicy, SpawnPolicyAuto)
	}
	if len(status.Agents) != 2 {
		t.Fatalf("Agents count = %d, want 2", len(status.Agents))
	}

	// Agents should be sorted by spawn time (oldest first).
	if status.Agents[0].ID != "blur_knife" {
		t.Errorf("Agents[0].ID = %q, want %q", status.Agents[0].ID, "blur_knife")
	}
	if status.Agents[0].TaskTitle != "Fix spawn() race condition" {
		t.Errorf("Agents[0].TaskTitle = %q, want %q", status.Agents[0].TaskTitle, "Fix spawn() race condition")
	}
	if status.Agents[0].LastLog != "Fixed spawn(), 3 tests remain" {
		t.Errorf("Agents[0].LastLog = %q, want %q", status.Agents[0].LastLog, "Fixed spawn(), 3 tests remain")
	}

	if status.Agents[1].ID != "sharp_proxy" {
		t.Errorf("Agents[1].ID = %q, want %q", status.Agents[1].ID, "sharp_proxy")
	}

	if len(status.Queue) != 2 {
		t.Fatalf("Queue count = %d, want 2", len(status.Queue))
	}
	if status.Queue[0].ID != "ts-ghi" {
		t.Errorf("Queue[0].ID = %q, want %q", status.Queue[0].ID, "ts-ghi")
	}
	if status.Queue[0].Priority != 1 {
		t.Errorf("Queue[0].Priority = %d, want 1", status.Queue[0].Priority)
	}
	if status.Queue[1].ID != "ts-jkl" {
		t.Errorf("Queue[1].ID = %q, want %q", status.Queue[1].ID, "ts-jkl")
	}

	if len(status.Errors) != 0 {
		t.Errorf("Errors = %v, want empty", status.Errors)
	}
}

func TestBuildFullStatusNoAgents(t *testing.T) {
	pool := statusPool(t, nil)
	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyAuto}

	readyOutput := "ID           PRI  TITLE\nts-aaa    1    Some task\n"
	runner := statusRunner(nil, readyOutput)

	status := BuildFullStatus(context.Background(), StatusSources{Pool: pool}, cfg, runner)

	if len(status.Agents) != 0 {
		t.Errorf("Agents count = %d, want 0", len(status.Agents))
	}
	if len(status.Queue) != 1 {
		t.Fatalf("Queue count = %d, want 1", len(status.Queue))
	}
	if status.SpawnPolicy != SpawnPolicyAuto {
		t.Errorf("SpawnPolicy = %q, want %q", status.SpawnPolicy, SpawnPolicyAuto)
	}
	if status.Queue[0].ID != "ts-aaa" {
		t.Errorf("Queue[0].ID = %q, want %q", status.Queue[0].ID, "ts-aaa")
	}
}

func TestBuildFullStatusManualPolicy(t *testing.T) {
	pool := statusPool(t, nil)
	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyManual}
	runner := statusRunner(nil, "ID           PRI  TITLE\n")

	status := BuildFullStatus(context.Background(), StatusSources{Pool: pool}, cfg, runner)
	if status.SpawnPolicy != SpawnPolicyManual {
		t.Errorf("SpawnPolicy = %q, want %q", status.SpawnPolicy, SpawnPolicyManual)
	}
}

func TestBuildFullStatusManualSkipsProgCalls(t *testing.T) {
	now := time.Now()
	agents := map[string]*Agent{
		"ts-abc": {
			ID:        "blur_knife",
			TaskID:    "ts-abc",
			Role:      RoleWorker,
			PID:       1234,
			SpawnTime: now,
			State:     AgentRunning,
		},
	}
	pool := statusPool(t, agents)
	cfg := Config{PoolSize: 3, SpawnPolicy: SpawnPolicyManual}

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("unexpected runner call in manual mode: %s %s", name, strings.Join(args, " "))
	}

	status := BuildFullStatus(context.Background(), StatusSources{Pool: pool}, cfg, runner)
	if len(status.Errors) != 0 {
		t.Fatalf("Errors = %v, want empty", status.Errors)
	}
	if len(status.Agents) != 1 {
		t.Fatalf("Agents count = %d, want 1", len(status.Agents))
	}
	if len(status.Queue) != 0 {
		t.Fatalf("Queue count = %d, want 0", len(status.Queue))
	}
}

func TestBuildFullStatusIncludesSpawnsWithoutPool(t *testing.T) {
	spawns := NewSpawnRegistry()
	if err := spawns.Register(SpawnEntry{
		SpawnID:   "spawn-1",
		PID:       999,
		State:     SpawnRunning,
		Prompt:    "test prompt",
		SpawnTime: time.Now(),
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	cfg := Config{PoolSize: 3, SpawnPolicy: SpawnPolicyManual}
	status := BuildFullStatus(context.Background(), StatusSources{Spawns: spawns}, cfg, nil)
	if len(status.Spawns) != 1 {
		t.Fatalf("Spawns count = %d, want 1", len(status.Spawns))
	}
}

func TestBuildFullStatusProgShowFails(t *testing.T) {
	now := time.Now()

	agents := map[string]*Agent{
		"ts-abc": {
			ID:        "blur_knife",
			TaskID:    "ts-abc",
			Role:      RoleWorker,
			PID:       1234,
			SpawnTime: now,
			State:     AgentRunning,
		},
	}

	// prog show will fail for ts-abc (not in map).
	runner := statusRunner(nil, "ID           PRI  TITLE\n")
	pool := statusPool(t, agents)
	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyAuto}

	status := BuildFullStatus(context.Background(), StatusSources{Pool: pool}, cfg, runner)

	// Agent should still appear, but with empty title/log.
	if len(status.Agents) != 1 {
		t.Fatalf("Agents count = %d, want 1", len(status.Agents))
	}
	if status.Agents[0].ID != "blur_knife" {
		t.Errorf("Agents[0].ID = %q, want %q", status.Agents[0].ID, "blur_knife")
	}
	if status.Agents[0].TaskTitle != "" {
		t.Errorf("Agents[0].TaskTitle = %q, want empty (prog show failed)", status.Agents[0].TaskTitle)
	}

	// Should have an error recorded.
	if len(status.Errors) != 1 {
		t.Fatalf("Errors count = %d, want 1", len(status.Errors))
	}
}

func TestBuildFullStatusProgReadyFails(t *testing.T) {
	now := time.Now()

	agents := map[string]*Agent{
		"ts-abc": {
			ID:        "blur_knife",
			TaskID:    "ts-abc",
			Role:      RoleWorker,
			PID:       1234,
			SpawnTime: now,
			State:     AgentRunning,
		},
	}

	showResponses := map[string]string{
		"ts-abc": `{"title":"Some task","logs":[]}`,
	}

	// Runner that succeeds for show but fails for ready.
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "show" {
			resp, ok := showResponses[args[1]]
			if !ok {
				return nil, fmt.Errorf("not found")
			}
			return []byte(resp), nil
		}
		if len(args) >= 1 && args[0] == "ready" {
			return nil, fmt.Errorf("prog ready failed")
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}

	pool := statusPool(t, agents)
	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyAuto}

	status := BuildFullStatus(context.Background(), StatusSources{Pool: pool}, cfg, runner)

	// Agents should still be populated.
	if len(status.Agents) != 1 {
		t.Fatalf("Agents count = %d, want 1", len(status.Agents))
	}
	if status.Agents[0].TaskTitle != "Some task" {
		t.Errorf("Agents[0].TaskTitle = %q, want %q", status.Agents[0].TaskTitle, "Some task")
	}

	// Queue should be empty, but error captured.
	if len(status.Queue) != 0 {
		t.Errorf("Queue count = %d, want 0", len(status.Queue))
	}
	if len(status.Errors) != 1 {
		t.Fatalf("Errors count = %d, want 1", len(status.Errors))
	}
}

func TestBuildFullStatusNoLogs(t *testing.T) {
	now := time.Now()

	agents := map[string]*Agent{
		"ts-abc": {
			ID:        "blur_knife",
			TaskID:    "ts-abc",
			Role:      RoleWorker,
			PID:       1234,
			SpawnTime: now,
			State:     AgentRunning,
		},
	}

	showResponses := map[string]string{
		"ts-abc": `{"title":"Task with no logs","logs":[]}`,
	}

	runner := statusRunner(showResponses, "ID           PRI  TITLE\n")
	pool := statusPool(t, agents)
	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyAuto}

	status := BuildFullStatus(context.Background(), StatusSources{Pool: pool}, cfg, runner)

	if len(status.Agents) != 1 {
		t.Fatalf("Agents count = %d, want 1", len(status.Agents))
	}
	if status.Agents[0].TaskTitle != "Task with no logs" {
		t.Errorf("TaskTitle = %q, want %q", status.Agents[0].TaskTitle, "Task with no logs")
	}
	if status.Agents[0].LastLog != "" {
		t.Errorf("LastLog = %q, want empty", status.Agents[0].LastLog)
	}
}

func TestBuildFullStatusWithSpawns(t *testing.T) {
	pool := statusPool(t, nil)
	cfg := Config{Project: "testproject", PoolSize: 3}
	runner := statusRunner(nil, "ID           PRI  TITLE\n")

	spawns := NewSpawnRegistry()
	_ = spawns.Register(SpawnEntry{
		SpawnID:   "spawn-ghost_wolf",
		PID:       9999,
		State:     SpawnRunning,
		Prompt:    "refactor auth module",
		SpawnTime: time.Now().Add(-5 * time.Minute),
	})
	_ = spawns.Register(SpawnEntry{
		SpawnID:   "spawn-neon_fox",
		PID:       8888,
		State:     SpawnRunning,
		Prompt:    "fix flaky test",
		SpawnTime: time.Now().Add(-2 * time.Minute),
	})

	status := BuildFullStatus(context.Background(), StatusSources{Pool: pool, Spawns: spawns}, cfg, runner)

	if len(status.Spawns) != 2 {
		t.Fatalf("Spawns count = %d, want 2", len(status.Spawns))
	}

	// Should be sorted by spawn time (oldest first).
	if status.Spawns[0].SpawnID != "spawn-ghost_wolf" {
		t.Errorf("Spawns[0].SpawnID = %q, want %q", status.Spawns[0].SpawnID, "spawn-ghost_wolf")
	}
	if status.Spawns[0].Prompt != "refactor auth module" {
		t.Errorf("Spawns[0].Prompt = %q, want %q", status.Spawns[0].Prompt, "refactor auth module")
	}
	if status.Spawns[1].SpawnID != "spawn-neon_fox" {
		t.Errorf("Spawns[1].SpawnID = %q, want %q", status.Spawns[1].SpawnID, "spawn-neon_fox")
	}
}

func TestBuildFullStatusWithRemoteSpawns(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()

	rspawns, err := OpenRemoteSpawnStore(dir)
	if err != nil {
		t.Fatalf("OpenRemoteSpawnStore() error = %v", err)
	}
	if err := rspawns.Upsert(RemoteSpawnRecord{
		SpawnID:   "sprites-abc",
		Provider:  "sprites",
		RequestID: "req-1",
		State:     RemoteSpawnRunning,
		SessionID: "sess-123",
		ServerRef: "https://sprites.dev/sandbox/abc",
		CreatedAt: now.Add(-10 * time.Minute),
		UpdatedAt: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := rspawns.Upsert(RemoteSpawnRecord{
		SpawnID:   "sprites-def",
		Provider:  "sprites",
		RequestID: "req-2",
		State:     RemoteSpawnRequested,
		CreatedAt: now.Add(-2 * time.Minute),
		UpdatedAt: now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	cfg := Config{PoolSize: 3, SpawnPolicy: SpawnPolicyManual}
	status := BuildFullStatus(context.Background(), StatusSources{RemoteSpawns: rspawns}, cfg, nil)

	if len(status.RemoteSpawns) != 2 {
		t.Fatalf("RemoteSpawns count = %d, want 2", len(status.RemoteSpawns))
	}

	// Should be sorted by creation time (oldest first).
	if status.RemoteSpawns[0].SpawnID != "sprites-abc" {
		t.Errorf("RemoteSpawns[0].SpawnID = %q, want %q", status.RemoteSpawns[0].SpawnID, "sprites-abc")
	}
	if status.RemoteSpawns[0].State != RemoteSpawnRunning {
		t.Errorf("RemoteSpawns[0].State = %q, want %q", status.RemoteSpawns[0].State, RemoteSpawnRunning)
	}
	if status.RemoteSpawns[0].Provider != "sprites" {
		t.Errorf("RemoteSpawns[0].Provider = %q, want %q", status.RemoteSpawns[0].Provider, "sprites")
	}
	if status.RemoteSpawns[0].SessionID != "sess-123" {
		t.Errorf("RemoteSpawns[0].SessionID = %q, want %q", status.RemoteSpawns[0].SessionID, "sess-123")
	}
	if status.RemoteSpawns[0].ServerRef != "https://sprites.dev/sandbox/abc" {
		t.Errorf("RemoteSpawns[0].ServerRef = %q, want %q", status.RemoteSpawns[0].ServerRef, "https://sprites.dev/sandbox/abc")
	}
	if status.RemoteSpawns[1].SpawnID != "sprites-def" {
		t.Errorf("RemoteSpawns[1].SpawnID = %q, want %q", status.RemoteSpawns[1].SpawnID, "sprites-def")
	}
	if status.RemoteSpawns[1].State != RemoteSpawnRequested {
		t.Errorf("RemoteSpawns[1].State = %q, want %q", status.RemoteSpawns[1].State, RemoteSpawnRequested)
	}
}

func TestRemoteSpawnStatusWireContract(t *testing.T) {
	// Verify that RemoteSpawnStatus (the wire type) does NOT include internal
	// fields that exist on RemoteSpawnRecord (request_id, provider_operation_id).
	// This guards the API boundary — if someone adds a field to the wire type
	// that should stay internal, this test fails.
	now := time.Now()
	wire := RemoteSpawnStatus{
		SpawnID:           "sprites-abc",
		Provider:          "sprites",
		ProviderSandboxID: "sandbox-123",
		ServerRef:         "https://sprites.dev/sandbox/abc",
		SessionID:         "sess-456",
		State:             RemoteSpawnRunning,
		LastError:         "",
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// These fields must NOT appear on the wire type.
	forbidden := []string{"request_id", "provider_operation_id"}
	for _, key := range forbidden {
		if _, ok := raw[key]; ok {
			t.Errorf("wire type RemoteSpawnStatus should not contain key %q, but it does", key)
		}
	}

	// These fields MUST appear.
	required := []string{"spawn_id", "provider", "state", "created_at", "updated_at"}
	for _, key := range required {
		if _, ok := raw[key]; !ok {
			t.Errorf("wire type RemoteSpawnStatus is missing required key %q", key)
		}
	}

	// Verify that the state constants used by daemon and client are in sync.
	// We test the string values since client uses plain strings while daemon uses typed strings.
	daemonStates := []RemoteSpawnState{
		RemoteSpawnRequested, RemoteSpawnSpawning, RemoteSpawnRunning,
		RemoteSpawnFailed, RemoteSpawnTerminated, RemoteSpawnUnknown,
	}
	expectedStrings := []string{
		"requested", "spawning", "running", "failed", "terminated", "unknown",
	}
	for i, ds := range daemonStates {
		if string(ds) != expectedStrings[i] {
			t.Errorf("daemon state %d = %q, want %q", i, string(ds), expectedStrings[i])
		}
	}
}

func TestTruncateLastError(t *testing.T) {
	short := "connection refused"
	if got := truncateLastError(short); got != short {
		t.Errorf("truncateLastError(%q) = %q, want unchanged", short, got)
	}

	long := strings.Repeat("x", 300)
	got := truncateLastError(long)
	if len(got) != maxLastErrorLen+len("...[truncated]") {
		t.Errorf("truncateLastError(300 chars) len = %d, want %d", len(got), maxLastErrorLen+len("...[truncated]"))
	}
	if !strings.HasSuffix(got, "...[truncated]") {
		t.Errorf("truncateLastError() should end with '...[truncated]', got suffix %q", got[len(got)-20:])
	}
}

func TestBuildFullStatusRemoteSpawnStoreError(t *testing.T) {
	dir := t.TempDir()
	rspawns, err := OpenRemoteSpawnStore(dir)
	if err != nil {
		t.Fatalf("OpenRemoteSpawnStore() error = %v", err)
	}

	// Write corrupted JSON to the store file.
	if err := os.WriteFile(rspawns.path, []byte(`{not json`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := Config{PoolSize: 3, SpawnPolicy: SpawnPolicyManual}
	status := BuildFullStatus(context.Background(), StatusSources{RemoteSpawns: rspawns}, cfg, nil)

	// RemoteSpawns should be empty (read failed), but error should be captured.
	if len(status.RemoteSpawns) != 0 {
		t.Errorf("RemoteSpawns count = %d, want 0", len(status.RemoteSpawns))
	}
	if len(status.Errors) != 1 {
		t.Fatalf("Errors count = %d, want 1", len(status.Errors))
	}
	if !strings.Contains(status.Errors[0], "remote spawn store") {
		t.Errorf("Error = %q, want to contain %q", status.Errors[0], "remote spawn store")
	}
}

func TestBuildFullStatusNilPool(t *testing.T) {
	cfg := Config{Project: "testproject", PoolSize: 3}
	runner := statusRunner(nil, "")

	status := BuildFullStatus(context.Background(), StatusSources{}, cfg, runner)

	if len(status.Agents) != 0 {
		t.Errorf("Agents count = %d, want 0", len(status.Agents))
	}
	if status.PoolSize != 3 {
		t.Errorf("PoolSize = %d, want 3", status.PoolSize)
	}
}
