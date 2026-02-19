package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

func TestBuildAgentDetailHappyPath(t *testing.T) {
	// Set up a pool with one running agent.
	logDir := t.TempDir()
	proc, release := newFakeProcess(1234)
	defer release()

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		return proc, nil
	}

	cfg := Config{
		Project:     "testproject",
		PoolSize:    2,
		SpawnCmd:    "fake-agent",
		PromptDir:   "",
		LogDir:      logDir,
		SpawnPolicy: SpawnPolicyAuto,
	}
	cfg.ApplyDefaults()

	runner := progRunnerWithShowJSON(`{
		"title": "Fix the auth bug",
		"logs": [{"message": "Tests passing, running review", "created_at": "2026-02-07T12:00:00Z"}]
	}`)

	pool := NewPool(cfg, runner, starter, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Fix auth"}}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	// Get the agent name from the pool (it's generated).
	agents := pool.Status()
	agentName := string(agents[0].ID)
	sessionID := "ses_test123"

	// Simulate session claim — set session ID on the agent.
	pool.SetSessionID(agentName, sessionID)

	// Push tool call events into the event buffer (replacing JSONL log writes).
	events := NewEventBuffer(DefaultEventBufSize)
	events.Push(SessionEvent{
		EventType: "message.part.updated",
		SessionID: sessionID,
		Timestamp: 1770499345000,
		Data:      json.RawMessage(`{"part":{"id":"prt_1","sessionID":"ses_test123","messageID":"msg_1","type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/project/auth.go"},"title":"auth.go","time":{"start":1770499344000,"end":1770499345000}}}}`),
	})
	events.Push(SessionEvent{
		EventType: "message.part.updated",
		SessionID: sessionID,
		Timestamp: 1770499346000,
		Data:      json.RawMessage(`{"part":{"id":"prt_2","sessionID":"ses_test123","messageID":"msg_1","type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"go test ./..."},"time":{"start":1770499345500,"end":1770499346000}}}}`),
	})

	// Build the detail.
	cfg.Runner = runner
	detail, err := BuildAgentDetail(ctx, pool, nil, events, cfg, runner, StatusAgentParams{
		AgentName: agentName,
	})
	if err != nil {
		t.Fatalf("BuildAgentDetail: %v", err)
	}

	// Verify agent metadata.
	if detail.ID != agentName {
		t.Errorf("ID = %q, want %q", detail.ID, agentName)
	}
	if detail.TaskID != "ts-abc" {
		t.Errorf("TaskID = %q, want %q", detail.TaskID, "ts-abc")
	}
	if detail.TaskTitle != "Fix the auth bug" {
		t.Errorf("TaskTitle = %q, want %q", detail.TaskTitle, "Fix the auth bug")
	}
	if detail.LastLog != "Tests passing, running review" {
		t.Errorf("LastLog = %q, want %q", detail.LastLog, "Tests passing, running review")
	}
	if detail.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", detail.SessionID, sessionID)
	}

	// Verify tool calls from event buffer.
	if len(detail.ToolCalls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(detail.ToolCalls))
	}
	if detail.ToolCalls[0].Tool != "read" {
		t.Errorf("call[0].Tool = %q, want %q", detail.ToolCalls[0].Tool, "read")
	}
	if detail.ToolCalls[0].Input != "/project/auth.go" {
		t.Errorf("call[0].Input = %q, want %q", detail.ToolCalls[0].Input, "/project/auth.go")
	}
	if detail.ToolCalls[1].Tool != "bash" {
		t.Errorf("call[1].Tool = %q, want %q", detail.ToolCalls[1].Tool, "bash")
	}

	if len(detail.Errors) != 0 {
		t.Errorf("unexpected errors: %v", detail.Errors)
	}
}

func TestBuildAgentDetailAgentNotFound(t *testing.T) {
	logDir := t.TempDir()
	cfg := Config{
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: "",
		LogDir:    logDir,
	}
	cfg.ApplyDefaults()

	pool := NewPool(cfg, nil, nil, testLogger())
	pool.ctx = context.Background()

	_, err := BuildAgentDetail(context.Background(), pool, nil, nil, cfg, nil, StatusAgentParams{
		AgentName: "nonexistent_agent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestBuildAgentDetailNilPool(t *testing.T) {
	_, err := BuildAgentDetail(context.Background(), nil, nil, nil, Config{}, nil, StatusAgentParams{
		AgentName: "some_agent",
	})
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}

func TestBuildAgentDetailNoSessionID(t *testing.T) {
	// Agent exists but has no session ID yet — tool calls should be empty.
	logDir := t.TempDir()
	proc, release := newFakeProcess(1234)
	defer release()

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		return proc, nil
	}

	cfg := Config{
		Project:     "testproject",
		PoolSize:    2,
		SpawnCmd:    "fake-agent",
		PromptDir:   "",
		LogDir:      logDir,
		SpawnPolicy: SpawnPolicyAuto,
	}
	cfg.ApplyDefaults()

	runner := progRunnerWithShowJSON(`{"title": "Some task", "logs": []}`)
	pool := NewPool(cfg, runner, starter, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Some task"}}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	agents := pool.Status()
	agentName := string(agents[0].ID)

	// Event buffer exists but agent has no session ID — no events to extract.
	events := NewEventBuffer(DefaultEventBufSize)

	detail, err := BuildAgentDetail(ctx, pool, nil, events, cfg, runner, StatusAgentParams{
		AgentName: agentName,
	})
	if err != nil {
		t.Fatalf("BuildAgentDetail: %v", err)
	}

	if len(detail.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls for agent without session ID, got %d", len(detail.ToolCalls))
	}
}

func TestBuildAgentDetailProgShowFails(t *testing.T) {
	// Prog show fails but tool calls still work — partial success.
	logDir := t.TempDir()
	proc, release := newFakeProcess(1234)
	defer release()

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		return proc, nil
	}

	cfg := Config{
		Project:     "testproject",
		PoolSize:    2,
		SpawnCmd:    "fake-agent",
		PromptDir:   "",
		LogDir:      logDir,
		SpawnPolicy: SpawnPolicyAuto,
	}
	cfg.ApplyDefaults()

	// Runner where prog show succeeds once (for FetchTaskMeta during spawn),
	// then fails on subsequent calls (for BuildAgentDetail).
	runner := progRunnerShowFailsAfterN(1)
	pool := NewPool(cfg, runner, starter, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Some task"}}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	agents := pool.Status()
	agentName := string(agents[0].ID)
	sessionID := "ses_test_fail"

	// Set session ID on the agent and push an event into the buffer.
	pool.SetSessionID(agentName, sessionID)
	events := NewEventBuffer(DefaultEventBufSize)
	events.Push(SessionEvent{
		EventType: "message.part.updated",
		SessionID: sessionID,
		Timestamp: 1770499345000,
		Data:      json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/foo"},"time":{"start":1,"end":2}}}}`),
	})

	detail, err := BuildAgentDetail(ctx, pool, nil, events, cfg, runner, StatusAgentParams{
		AgentName: agentName,
	})
	if err != nil {
		t.Fatalf("BuildAgentDetail: %v", err)
	}

	// Tool calls should still be populated from event buffer.
	if len(detail.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(detail.ToolCalls))
	}

	// Should have an error from prog show.
	if len(detail.Errors) == 0 {
		t.Error("expected error from failed prog show")
	}
}

func TestBuildAgentDetailSpawnWithEvents(t *testing.T) {
	// Spawned agent found via spawn registry, tool calls come from event buffer.
	sessionID := "ses_spawn_test"

	spawns := NewSpawnRegistry()
	_ = spawns.Register(SpawnEntry{
		SpawnID:   "spawn-abc",
		PID:       9999,
		State:     SpawnRunning,
		SessionID: sessionID,
		Prompt:    "fix the authentication bug in the login flow",
	})

	events := NewEventBuffer(DefaultEventBufSize)
	events.Push(SessionEvent{
		EventType: "message.part.updated",
		SessionID: sessionID,
		Timestamp: 1000,
		Data:      json.RawMessage(`{"part":{"id":"prt_1","type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/src/auth.go"},"title":"auth.go","time":{"start":500,"end":1000}}}}`),
	})
	events.Push(SessionEvent{
		EventType: "message.part.updated",
		SessionID: sessionID,
		Timestamp: 2000,
		Data:      json.RawMessage(`{"part":{"id":"prt_2","type":"tool","tool":"edit","state":{"status":"completed","input":{"filePath":"/src/auth.go"},"title":"auth.go","time":{"start":1500,"end":2000}}}}`),
	})

	detail, err := BuildAgentDetail(context.Background(), nil, spawns, events, Config{}, nil, StatusAgentParams{
		AgentName: "spawn-abc",
	})
	if err != nil {
		t.Fatalf("BuildAgentDetail for spawn: %v", err)
	}

	if detail.ID != "spawn-abc" {
		t.Errorf("ID = %q, want %q", detail.ID, "spawn-abc")
	}
	if detail.Role != string(RoleSpawn) {
		t.Errorf("Role = %q, want %q", detail.Role, RoleSpawn)
	}
	if detail.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", detail.SessionID, sessionID)
	}
	if detail.TaskTitle != "fix the authentication bug in the login flow" {
		t.Errorf("TaskTitle = %q, want prompt text", detail.TaskTitle)
	}

	if len(detail.ToolCalls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(detail.ToolCalls))
	}
	if detail.ToolCalls[0].Tool != "read" {
		t.Errorf("call[0].Tool = %q, want %q", detail.ToolCalls[0].Tool, "read")
	}
	if detail.ToolCalls[1].Tool != "edit" {
		t.Errorf("call[1].Tool = %q, want %q", detail.ToolCalls[1].Tool, "edit")
	}

	if len(detail.Errors) != 0 {
		t.Errorf("unexpected errors: %v", detail.Errors)
	}
}

func TestBuildAgentDetailSpawnNoSessionID(t *testing.T) {
	// Spawned agent with no session ID — tool calls should be empty.
	spawns := NewSpawnRegistry()
	_ = spawns.Register(SpawnEntry{
		SpawnID: "spawn-nostream",
		PID:     9999,
		State:   SpawnRunning,
		Prompt:  "do something",
	})

	events := NewEventBuffer(DefaultEventBufSize)

	detail, err := BuildAgentDetail(context.Background(), nil, spawns, events, Config{}, nil, StatusAgentParams{
		AgentName: "spawn-nostream",
	})
	if err != nil {
		t.Fatalf("BuildAgentDetail for spawn: %v", err)
	}

	if detail.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", detail.SessionID)
	}
	if len(detail.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(detail.ToolCalls))
	}
}

// --- test helpers ---

// testLogger returns a discarding logger for tests.
func testLogger() *slog.Logger {
	return slog.Default()
}

// progRunnerWithShowJSON returns a CommandRunner where prog show returns the given JSON.
func progRunnerWithShowJSON(showJSON string) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "start" {
			return []byte("Started"), nil
		}
		if len(args) >= 1 && args[0] == "show" {
			return []byte(showJSON), nil
		}
		return nil, fmt.Errorf("unexpected command: %s %v", name, args)
	}
}

// progRunnerShowFailsAfterN returns a CommandRunner where prog show succeeds
// the first n times (for spawn's FetchTaskMeta) then fails (for BuildAgentDetail).
func progRunnerShowFailsAfterN(n int) CommandRunner {
	var count int
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "start" {
			return []byte("Started"), nil
		}
		if len(args) >= 1 && args[0] == "show" {
			count++
			if count <= n {
				return []byte(`{"id":"ts-abc","type":"task","definition_of_done":"Do it","labels":[],"title":"Some task","logs":[]}`), nil
			}
			return nil, fmt.Errorf("network timeout")
		}
		return nil, fmt.Errorf("unexpected command: %s %v", name, args)
	}
}
