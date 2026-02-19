package daemon

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestHandleLogsPathHappyPath(t *testing.T) {
	logDir := t.TempDir()
	proc, release := newFakeProcess(1234)
	defer release()

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		return proc, nil
	}

	cfg := Config{
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: "",
		LogDir:    logDir,
	}
	cfg.ApplyDefaults()

	runner := progRunner(testTaskMeta)
	pool := NewPool(cfg, runner, starter, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	agents := pool.Status()
	agentName := string(agents[0].ID)

	d := &Daemon{
		config: cfg,
		pool:   pool,
		log:    testLogger(),
	}

	params, _ := json.Marshal(LogsPathParams{AgentName: agentName})
	resp := d.handleLogsPath(params)

	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result LogsPathResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	want := logFilePath(logDir, "ts-abc")
	if result.Path != want {
		t.Errorf("path = %q, want %q", result.Path, want)
	}
}

func TestHandleLogsPathMissingAgentName(t *testing.T) {
	d := &Daemon{
		config: Config{},
		log:    testLogger(),
	}

	resp := d.handleLogsPath(nil)
	if resp.Success {
		t.Fatal("expected error for missing agent_name")
	}
	if resp.Error != "agent_name is required" {
		t.Errorf("error = %q, want %q", resp.Error, "agent_name is required")
	}
}

func TestHandleLogsPathNilPool(t *testing.T) {
	d := &Daemon{
		config: Config{},
		pool:   nil,
		log:    testLogger(),
	}

	params, _ := json.Marshal(LogsPathParams{AgentName: "some_agent"})
	resp := d.handleLogsPath(params)
	if resp.Success {
		t.Fatal("expected error for nil pool")
	}
	if resp.Error != `agent "some_agent" not found` {
		t.Errorf("error = %q, want %q", resp.Error, `agent "some_agent" not found`)
	}
}

func TestHandleLogsPathAgentNotFound(t *testing.T) {
	cfg := Config{
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: "",
		LogDir:    t.TempDir(),
	}
	cfg.ApplyDefaults()

	pool := NewPool(cfg, nil, nil, testLogger())
	pool.ctx = context.Background()

	d := &Daemon{
		config: cfg,
		pool:   pool,
		log:    testLogger(),
	}

	params, _ := json.Marshal(LogsPathParams{AgentName: "nonexistent_agent"})
	resp := d.handleLogsPath(params)
	if resp.Success {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestHandleLogsPathSpawnFallback(t *testing.T) {
	spawns := NewSpawnRegistry()
	_ = spawns.Register(SpawnEntry{
		SpawnID: "spawn-test_agent",
		PID:     5678,
		State:   SpawnRunning,
		Prompt:  "fix the tests",
		LogPath: "/tmp/logs/spawn-test_agent.jsonl",
	})

	d := &Daemon{
		config: Config{},
		pool:   nil, // no pool — spawn registry only
		spawns: spawns,
		log:    testLogger(),
	}

	params, _ := json.Marshal(LogsPathParams{AgentName: "spawn-test_agent"})
	resp := d.handleLogsPath(params)

	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result LogsPathResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.Path != "/tmp/logs/spawn-test_agent.jsonl" {
		t.Errorf("path = %q, want %q", result.Path, "/tmp/logs/spawn-test_agent.jsonl")
	}
}

func TestHandleLogsPathInvalidParams(t *testing.T) {
	d := &Daemon{
		config: Config{},
		log:    testLogger(),
	}

	resp := d.handleLogsPath(json.RawMessage(`{invalid json`))
	if resp.Success {
		t.Fatal("expected error for invalid params")
	}
}
