package daemon

import (
	"context"
	"encoding/json"
	"io"
	"syscall"
	"testing"
	"time"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

func TestHandleAgentKillHappyPath(t *testing.T) {
	cfg := Config{
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: "",
		LogDir:    t.TempDir(),
	}
	cfg.ApplyDefaults()

	proc, release := newFakeProcess(9999)
	defer release()

	// Track if SIGTERM was sent to the correct PID
	killedPID := 0
	killedSignal := syscall.Signal(0)

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		return proc, nil
	}

	pool := NewPool(cfg, progRunner(testTaskMeta), starter, testLogger())
	pool.ctx = context.Background()

	// Inject a signal sender that tracks calls
	origKillFunc := syscallKill
	defer func() { syscallKill = origKillFunc }()
	syscallKill = func(pid int, sig syscall.Signal) error {
		killedPID = pid
		killedSignal = sig
		return nil
	}

	// Spawn a fake agent
	pool.mu.Lock()
	agentID := protocol.AgentID("test-agent-1")
	pool.agents["ts-abc"] = &Agent{
		ID:        agentID,
		TaskID:    "ts-abc",
		Role:      RoleWorker,
		PID:       9999,
		SpawnTime: time.Now(),
		State:     AgentRunning,
	}
	pool.mu.Unlock()

	d := &Daemon{config: cfg, pool: pool, log: testLogger()}

	// Build request params
	params, _ := json.Marshal(AgentKillParams{AgentName: "test-agent-1"})

	resp := d.handleAgentKill(json.RawMessage(params))
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result AgentKillResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.AgentName != "test-agent-1" {
		t.Errorf("agent_name = %q, want %q", result.AgentName, "test-agent-1")
	}
	if result.PID != 9999 {
		t.Errorf("pid = %d, want %d", result.PID, 9999)
	}

	// Verify SIGTERM was sent to the correct PID
	if killedPID != 9999 {
		t.Errorf("killed PID = %d, want %d", killedPID, 9999)
	}
	if killedSignal != syscall.SIGTERM {
		t.Errorf("killed signal = %v, want %v", killedSignal, syscall.SIGTERM)
	}
}

func TestHandleAgentKillAgentNotFound(t *testing.T) {
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

	d := &Daemon{config: cfg, pool: pool, log: testLogger()}

	// Build request params for non-existent agent
	params, _ := json.Marshal(AgentKillParams{AgentName: "non-existent"})

	resp := d.handleAgentKill(json.RawMessage(params))
	if resp.Success {
		t.Fatal("expected error for non-existent agent")
	}
	if resp.Error != "agent not found: non-existent" {
		t.Errorf("error = %q, want %q", resp.Error, "agent not found: non-existent")
	}
}

func TestHandleAgentKillNilPool(t *testing.T) {
	d := &Daemon{config: Config{}, pool: nil, log: testLogger()}

	params, _ := json.Marshal(AgentKillParams{AgentName: "test-agent"})
	resp := d.handleAgentKill(json.RawMessage(params))

	if resp.Success {
		t.Error("expected error for nil pool")
	}
	if resp.Error != "no pool configured" {
		t.Errorf("error = %q, want %q", resp.Error, "no pool configured")
	}
}

func TestHandleAgentKillInvalidParams(t *testing.T) {
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

	d := &Daemon{config: cfg, pool: pool, log: testLogger()}

	// Test with invalid JSON
	resp := d.handleAgentKill(json.RawMessage(`{invalid json}`))
	if resp.Success {
		t.Error("expected error for invalid JSON")
	}

	// Test with missing agent_name
	params, _ := json.Marshal(AgentKillParams{})
	resp = d.handleAgentKill(json.RawMessage(params))
	if resp.Success {
		t.Error("expected error for missing agent_name")
	}
	if resp.Error != "agent_name is required" {
		t.Errorf("error = %q, want %q", resp.Error, "agent_name is required")
	}
}

func TestHandleAgentKillSignalError(t *testing.T) {
	cfg := Config{
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: "",
		LogDir:    t.TempDir(),
	}
	cfg.ApplyDefaults()

	proc, release := newFakeProcess(9999)
	defer release()

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		return proc, nil
	}

	pool := NewPool(cfg, progRunner(testTaskMeta), starter, testLogger())
	pool.ctx = context.Background()

	// Inject a signal sender that returns an error
	origKillFunc := syscallKill
	defer func() { syscallKill = origKillFunc }()
	syscallKill = func(pid int, sig syscall.Signal) error {
		return syscall.EPERM
	}

	// Spawn a fake agent
	pool.mu.Lock()
	agentID := protocol.AgentID("test-agent-1")
	pool.agents["ts-abc"] = &Agent{
		ID:        agentID,
		TaskID:    "ts-abc",
		Role:      RoleWorker,
		PID:       9999,
		SpawnTime: time.Now(),
		State:     AgentRunning,
	}
	pool.mu.Unlock()

	d := &Daemon{config: cfg, pool: pool, log: testLogger()}

	// Build request params
	params, _ := json.Marshal(AgentKillParams{AgentName: "test-agent-1"})

	resp := d.handleAgentKill(json.RawMessage(params))
	if resp.Success {
		t.Fatal("expected error when signal fails")
	}
	if resp.Error != "failed to send SIGTERM to PID 9999: operation not permitted" {
		t.Errorf("error = %q, want permission error", resp.Error)
	}
}
