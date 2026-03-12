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

	"github.com/baiirun/aetherflow/internal/sessions"
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

	status := BuildFullStatus(context.Background(), pool, nil, nil, nil, cfg, runner)

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

	status := BuildFullStatus(context.Background(), pool, nil, nil, nil, cfg, runner)

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

	status := BuildFullStatus(context.Background(), pool, nil, nil, nil, cfg, runner)
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

	status := BuildFullStatus(context.Background(), pool, nil, nil, nil, cfg, runner)
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
	status := BuildFullStatus(context.Background(), nil, spawns, nil, nil, cfg, nil)
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

	status := BuildFullStatus(context.Background(), pool, nil, nil, nil, cfg, runner)

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

	status := BuildFullStatus(context.Background(), pool, nil, nil, nil, cfg, runner)

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

	status := BuildFullStatus(context.Background(), pool, nil, nil, nil, cfg, runner)

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

	status := BuildFullStatus(context.Background(), pool, spawns, nil, nil, cfg, runner)

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

func TestBuildFullStatusNilPool(t *testing.T) {
	cfg := Config{Project: "testproject", PoolSize: 3}
	runner := statusRunner(nil, "")

	status := BuildFullStatus(context.Background(), nil, nil, nil, nil, cfg, runner)
	if len(status.Agents) != 0 {
		t.Errorf("Agents count = %d, want 0", len(status.Agents))
	}
	if status.PoolSize != 3 {
		t.Errorf("PoolSize = %d, want 3", status.PoolSize)
	}
}

func TestBuildFullStatusAddsSessionSummaryFields(t *testing.T) {
	now := time.Now()
	sessionDir := t.TempDir()
	store, err := sessions.Open(sessionDir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Upsert(sessions.Record{
		ServerRef:  "http://127.0.0.1:4096",
		SessionID:  "ses_agent",
		Origin:     sessions.OriginPool,
		WorkRef:    "ts-abc",
		Status:     sessions.StatusIdle,
		LastSeenAt: now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("Upsert(agent) error = %v", err)
	}
	if err := store.Upsert(sessions.Record{
		ServerRef:  "http://127.0.0.1:4096",
		SessionID:  "ses_spawn",
		Origin:     sessions.OriginSpawn,
		WorkRef:    "spawn-1",
		Status:     sessions.StatusTerminated,
		LastSeenAt: now.Add(-3 * time.Minute),
	}); err != nil {
		t.Fatalf("Upsert(spawn) error = %v", err)
	}

	events := NewEventBuffer(16)
	events.Push(SessionEvent{
		EventType: "message.part.updated",
		SessionID: "ses_agent",
		Timestamp: now.Add(-30 * time.Second).UnixMilli(),
		Data:      json.RawMessage(`{"part":{"type":"text","text":"working"}}`),
	})

	agents := map[string]*Agent{
		"ts-abc": {
			ID:        "blur_knife",
			TaskID:    "ts-abc",
			Role:      RoleWorker,
			PID:       1234,
			SessionID: "ses_agent",
			SpawnTime: now.Add(-10 * time.Minute),
			State:     AgentRunning,
		},
	}
	pool := statusPool(t, agents)
	spawns := NewSpawnRegistry()
	if err := spawns.Register(SpawnEntry{
		SpawnID:   "spawn-1",
		PID:       99,
		SessionID: "ses_spawn",
		State:     SpawnExited,
		Prompt:    "investigate failure",
		SpawnTime: now.Add(-20 * time.Minute),
		ExitedAt:  now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyManual, ServerURL: "http://127.0.0.1:4096"}
	status := BuildFullStatus(context.Background(), pool, spawns, store, events, cfg, nil)

	if got := status.Agents[0].State; got != string(AgentRunning) {
		t.Fatalf("Agents[0].State = %q, want %q", got, AgentRunning)
	}
	if got := status.Agents[0].LifecycleState; got != string(AgentRunning) {
		t.Fatalf("Agents[0].LifecycleState = %q, want %q", got, AgentRunning)
	}
	wantAgentLastActivity := now.Add(-30 * time.Second).UnixMilli()
	if got := status.Agents[0].LastActivityAt.UnixMilli(); got != wantAgentLastActivity {
		t.Fatalf("Agents[0].LastActivityAt = %d, want %d", got, wantAgentLastActivity)
	}
	if status.Agents[0].AttentionNeeded {
		t.Fatal("Agents[0].AttentionNeeded = true, want false")
	}

	if got := status.Spawns[0].State; got != SpawnExited {
		t.Fatalf("Spawns[0].State = %q, want %q", got, SpawnExited)
	}
	if got := status.Spawns[0].LifecycleState; got != string(SpawnExited) {
		t.Fatalf("Spawns[0].LifecycleState = %q, want %q", got, SpawnExited)
	}
	if !status.Spawns[0].AttentionNeeded {
		t.Fatal("Spawns[0].AttentionNeeded = false, want true")
	}
	wantSpawnLastActivity := now.Add(-3 * time.Minute).UnixMilli()
	if got := status.Spawns[0].LastActivityAt.UnixMilli(); got != wantSpawnLastActivity {
		t.Fatalf("Spawns[0].LastActivityAt = %d, want %d", got, wantSpawnLastActivity)
	}
}

func TestBuildFullStatusUsesServerScopedSessionIdentity(t *testing.T) {
	now := time.Now()
	sessionDir := t.TempDir()
	store, err := sessions.Open(sessionDir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Upsert(sessions.Record{
		ServerRef:  "http://wrong-server:4096",
		SessionID:  "ses_shared",
		Origin:     sessions.OriginPool,
		WorkRef:    "ts-abc",
		Status:     sessions.StatusTerminated,
		LastSeenAt: now.Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("Upsert(wrong) error = %v", err)
	}
	if err := store.Upsert(sessions.Record{
		ServerRef:  "http://right-server:4096",
		SessionID:  "ses_shared",
		Origin:     sessions.OriginPool,
		WorkRef:    "ts-abc",
		Status:     sessions.StatusIdle,
		LastSeenAt: now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("Upsert(right) error = %v", err)
	}

	agents := map[string]*Agent{
		"ts-abc": {
			ID:        "blur_knife",
			TaskID:    "ts-abc",
			Role:      RoleWorker,
			PID:       1234,
			SessionID: "ses_shared",
			SpawnTime: now.Add(-10 * time.Minute),
			State:     AgentRunning,
		},
	}
	pool := statusPool(t, agents)

	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyManual, ServerURL: "http://right-server:4096"}
	status := BuildFullStatus(context.Background(), pool, nil, store, nil, cfg, nil)

	if got := status.Agents[0].LifecycleState; got != string(AgentRunning) {
		t.Fatalf("Agents[0].LifecycleState = %q, want %q", got, AgentRunning)
	}
	if status.Agents[0].AttentionNeeded {
		t.Fatal("Agents[0].AttentionNeeded = true, want false")
	}
}

func TestBuildFullStatusClearsAttentionForIdleExitedSpawn(t *testing.T) {
	now := time.Now()
	sessionDir := t.TempDir()
	store, err := sessions.Open(sessionDir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Upsert(sessions.Record{
		ServerRef:  "http://127.0.0.1:4096",
		SessionID:  "ses_spawn_idle",
		Origin:     sessions.OriginSpawn,
		WorkRef:    "spawn-idle",
		Status:     sessions.StatusIdle,
		LastSeenAt: now.Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	spawns := NewSpawnRegistry()
	if err := spawns.Register(SpawnEntry{
		SpawnID:   "spawn-idle",
		PID:       99,
		SessionID: "ses_spawn_idle",
		State:     SpawnExited,
		Prompt:    "completed cleanly",
		SpawnTime: now.Add(-5 * time.Minute),
		ExitedAt:  now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyManual, ServerURL: "http://127.0.0.1:4096"}
	status := BuildFullStatus(context.Background(), nil, spawns, store, nil, cfg, nil)

	if got := status.Spawns[0].LifecycleState; got != string(SpawnExited) {
		t.Fatalf("Spawns[0].LifecycleState = %q, want %q", got, SpawnExited)
	}
	if status.Spawns[0].AttentionNeeded {
		t.Fatal("Spawns[0].AttentionNeeded = true, want false")
	}
}

func TestBuildFullStatusSurfacesSessionIndexErrors(t *testing.T) {
	pool := statusPool(t, nil)
	cfg := Config{Project: "testproject", PoolSize: 3, SpawnPolicy: SpawnPolicyManual, ServerURL: "http://127.0.0.1:4096"}

	sessionDir := t.TempDir()
	store, err := sessions.Open(sessionDir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := os.WriteFile(store.Path(), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	status := BuildFullStatus(context.Background(), pool, nil, store, nil, cfg, nil)
	if len(status.Errors) != 1 {
		t.Fatalf("Errors count = %d, want 1", len(status.Errors))
	}
	if !strings.Contains(status.Errors[0], "session index:") {
		t.Fatalf("Errors[0] = %q, want session index prefix", status.Errors[0])
	}
}
