package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: testPromptDir(t),
		LogDir:    logDir,
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

	// Write a JSONL log file for the agent's task.
	logPath := filepath.Join(logDir, "ts-abc.jsonl")
	lines := []string{
		`{"type":"tool_use","timestamp":1770499345000,"sessionID":"ses_1","part":{"tool":"read","title":"auth.go","state":{"status":"completed","input":{"filePath":"/project/auth.go"},"time":{"start":1770499344000,"end":1770499345000}}}}`,
		`{"type":"tool_use","timestamp":1770499346000,"sessionID":"ses_1","part":{"tool":"bash","state":{"status":"completed","input":{"command":"go test ./..."},"time":{"start":1770499345500,"end":1770499346000}}}}`,
	}
	writeLines(t, logPath, lines)

	// Get the agent name from the pool (it's generated).
	agents := pool.Status()
	agentName := string(agents[0].ID)

	// Build the detail.
	cfg.Runner = runner
	detail, err := BuildAgentDetail(ctx, pool, cfg, runner, StatusAgentParams{
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

	// Verify tool calls.
	if len(detail.ToolCalls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(detail.ToolCalls))
	}
	if detail.ToolCalls[0].Tool != "read" {
		t.Errorf("call[0].Tool = %q, want %q", detail.ToolCalls[0].Tool, "read")
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
		PromptDir: testPromptDir(t),
		LogDir:    logDir,
	}
	cfg.ApplyDefaults()

	pool := NewPool(cfg, nil, nil, testLogger())
	pool.ctx = context.Background()

	_, err := BuildAgentDetail(context.Background(), pool, cfg, nil, StatusAgentParams{
		AgentName: "nonexistent_agent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestBuildAgentDetailNilPool(t *testing.T) {
	_, err := BuildAgentDetail(context.Background(), nil, Config{}, nil, StatusAgentParams{
		AgentName: "some_agent",
	})
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}

func TestBuildAgentDetailNoLogFile(t *testing.T) {
	// Agent exists but no JSONL file — tool calls should be empty, not an error.
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
		PromptDir: testPromptDir(t),
		LogDir:    logDir,
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

	// Delete the log file that spawn created (to simulate no log data).
	os.Remove(filepath.Join(logDir, "ts-abc.jsonl"))

	agents := pool.Status()
	agentName := string(agents[0].ID)

	detail, err := BuildAgentDetail(ctx, pool, cfg, runner, StatusAgentParams{
		AgentName: agentName,
	})
	if err != nil {
		t.Fatalf("BuildAgentDetail: %v", err)
	}

	if len(detail.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls for missing log, got %d", len(detail.ToolCalls))
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
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: testPromptDir(t),
		LogDir:    logDir,
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

	// Write a log file so tool calls succeed.
	logPath := filepath.Join(logDir, "ts-abc.jsonl")
	lines := []string{
		`{"type":"tool_use","timestamp":1770499345000,"sessionID":"ses_1","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/foo"},"time":{"start":1,"end":2}}}}`,
	}
	writeLines(t, logPath, lines)

	agents := pool.Status()
	agentName := string(agents[0].ID)

	detail, err := BuildAgentDetail(ctx, pool, cfg, runner, StatusAgentParams{
		AgentName: agentName,
	})
	if err != nil {
		t.Fatalf("BuildAgentDetail: %v", err)
	}

	// Tool calls should still be populated.
	if len(detail.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(detail.ToolCalls))
	}

	// Should have an error from prog show.
	if len(detail.Errors) == 0 {
		t.Error("expected error from failed prog show")
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
