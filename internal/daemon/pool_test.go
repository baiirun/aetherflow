package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProcess implements Process for testing.
type fakeProcess struct {
	pid    int
	waitCh chan struct{} // close to make Wait() return
	err    error         // returned by Wait()
}

func (p *fakeProcess) Wait() error {
	<-p.waitCh
	return p.err
}

func (p *fakeProcess) PID() int {
	return p.pid
}

// newFakeProcess creates a process that blocks until release() is called.
func newFakeProcess(pid int) (*fakeProcess, func()) {
	p := &fakeProcess{pid: pid, waitCh: make(chan struct{})}
	return p, func() { close(p.waitCh) }
}

// newFakeProcessWithError creates a process that returns an error on Wait.
func newFakeProcessWithError(pid int, err error) (*fakeProcess, func()) {
	p := &fakeProcess{pid: pid, waitCh: make(chan struct{}), err: err}
	return p, func() { close(p.waitCh) }
}

// testPromptDir creates a temp directory with a worker.md template for testing.
// Only worker.md is created because InferRole() always returns RoleWorker (MVP).
// Add planner.md here when planner role inference is implemented.
func testPromptDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := "# Worker\n\nTask: {{task_id}}\n"
	if err := os.WriteFile(filepath.Join(dir, "worker.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// testPool creates a pool with sensible test defaults and the given fakes.
func testPool(t *testing.T, runner CommandRunner, starter ProcessStarter) *Pool {
	t.Helper()
	cfg := Config{
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: testPromptDir(t),
	}
	cfg.ApplyDefaults()

	return NewPool(cfg, runner, starter, slog.Default())
}

// progRunner returns a CommandRunner that handles prog start and prog show.
// It accepts all starts and returns a fixed JSON for show.
func progRunner(taskMeta string) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "start" {
			return []byte("Started"), nil
		}
		if len(args) >= 1 && args[0] == "show" {
			return []byte(taskMeta), nil
		}
		return nil, fmt.Errorf("unexpected command: %s %v", name, args)
	}
}

const testTaskMeta = `{
	"id": "ts-abc",
	"type": "task",
	"definition_of_done": "Tests pass",
	"labels": []
}`

func TestPoolScheduleSpawnsAgent(t *testing.T) {
	proc, release := newFakeProcess(1234)
	defer release()

	var spawnedPrompt string
	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		spawnedPrompt = prompt
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for the agent to appear in the pool.
	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	agents := pool.Status()
	if agents[0].TaskID != "ts-abc" {
		t.Errorf("TaskID = %q, want %q", agents[0].TaskID, "ts-abc")
	}
	if agents[0].State != AgentRunning {
		t.Errorf("State = %q, want %q", agents[0].State, AgentRunning)
	}
	if agents[0].PID != 1234 {
		t.Errorf("PID = %d, want %d", agents[0].PID, 1234)
	}

	// Verify the rendered prompt contains the task ID (not the template variable).
	if !strings.Contains(spawnedPrompt, "ts-abc") {
		t.Errorf("prompt should contain task ID, got: %q", spawnedPrompt)
	}
	if strings.Contains(spawnedPrompt, "{{task_id}}") {
		t.Error("prompt should not contain unreplaced {{task_id}}")
	}
}

func TestPoolSkipsAlreadyRunning(t *testing.T) {
	proc, release := newFakeProcess(1234)
	defer release()

	var spawnCount atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		spawnCount.Add(1)
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 2)
	// Send same task twice.
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for first spawn.
	waitFor(t, func() bool {
		return spawnCount.Load() >= 1
	})

	// Give the second batch time to be processed.
	time.Sleep(50 * time.Millisecond)

	// Should only have spawned once.
	if got := spawnCount.Load(); got != 1 {
		t.Errorf("spawn count = %d, want 1", got)
	}
}

func TestPoolRespectsPoolSize(t *testing.T) {
	var spawnCount atomic.Int32

	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		spawnCount.Add(1)
		proc, _ := newFakeProcess(100)
		return proc, nil
	}

	// Different task meta for each task.
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "start" {
			return []byte("Started"), nil
		}
		if len(args) >= 2 && args[0] == "show" {
			meta := fmt.Sprintf(`{"id":"%s","type":"task","definition_of_done":"Do it","labels":[]}`, args[1])
			return []byte(meta), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}

	pool := testPool(t, runner, starter) // pool size = 2

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	// Send 3 tasks, but pool size is 2.
	taskCh <- []Task{
		{ID: "ts-1", Priority: 1, Title: "First"},
		{ID: "ts-2", Priority: 1, Title: "Second"},
		{ID: "ts-3", Priority: 1, Title: "Third"},
	}

	go pool.Run(ctx, taskCh)

	// Wait for 2 agents to spawn.
	waitFor(t, func() bool {
		return len(pool.Status()) == 2
	})

	// Should NOT have spawned the third.
	if len(pool.Status()) != 2 {
		t.Fatalf("running agents = %d, want 2", len(pool.Status()))
	}
}

func TestPoolReapsExitedProcess(t *testing.T) {
	proc, release := newFakeProcess(1234)

	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for spawn.
	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	// Release the process (simulate clean exit).
	release()

	// Wait for reap.
	waitFor(t, func() bool {
		return len(pool.Status()) == 0
	})
}

func TestPoolReapsProcessWithError(t *testing.T) {
	proc, release := newFakeProcessWithError(1234, fmt.Errorf("exit status 1"))

	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	release()

	waitFor(t, func() bool {
		return len(pool.Status()) == 0
	})
}

func TestPoolSaveAndLoadState(t *testing.T) {
	proc, release := newFakeProcess(1234)
	defer release()

	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	// Save state.
	dir := t.TempDir()
	path := filepath.Join(dir, "pool-state.json")

	if err := pool.SaveState(path); err != nil {
		t.Fatalf("SaveState error: %v", err)
	}

	// Verify file is valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}

	var state PoolState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("parsing state file: %v", err)
	}

	if len(state.Agents) != 1 {
		t.Fatalf("state has %d agents, want 1", len(state.Agents))
	}
	if state.Agents[0].TaskID != "ts-abc" {
		t.Errorf("state agent TaskID = %q, want %q", state.Agents[0].TaskID, "ts-abc")
	}

	// Load it back.
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(loaded.Agents) != 1 {
		t.Fatalf("loaded %d agents, want 1", len(loaded.Agents))
	}
}

func TestLoadStateMissingFile(t *testing.T) {
	state, err := LoadState("/nonexistent/pool-state.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if len(state.Agents) != 0 {
		t.Errorf("expected empty state, got %d agents", len(state.Agents))
	}
}

func TestPoolStatus(t *testing.T) {
	proc, release := newFakeProcess(1234)
	defer release()

	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)

	// Initially empty.
	if got := pool.Status(); len(got) != 0 {
		t.Errorf("initial status has %d agents, want 0", len(got))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	agents := pool.Status()
	if agents[0].TaskID != "ts-abc" {
		t.Errorf("TaskID = %q, want %q", agents[0].TaskID, "ts-abc")
	}
	if agents[0].Role != RoleWorker {
		t.Errorf("Role = %q, want %q", agents[0].Role, RoleWorker)
	}
}

func TestPoolProgStartFailure(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "start" {
			return []byte("already in progress"), fmt.Errorf("exit status 1")
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}

	var spawned atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		spawned.Add(1)
		proc, _ := newFakeProcess(1)
		return proc, nil
	}

	pool := testPool(t, runner, starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	time.Sleep(50 * time.Millisecond)

	// Should not have spawned since prog start failed.
	if got := spawned.Load(); got != 0 {
		t.Errorf("spawn count = %d, want 0", got)
	}
	if got := len(pool.Status()); got != 0 {
		t.Errorf("running agents = %d, want 0", got)
	}
}

// --- crash detection and respawn tests ---

func TestCrashRespawnsAgent(t *testing.T) {
	var spawnCount atomic.Int32
	var mu sync.Mutex
	procs := make([]*fakeProcess, 0)
	releases := make([]func(), 0)

	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		spawnCount.Add(1)
		proc, release := newFakeProcess(int(spawnCount.Load()) * 100)
		mu.Lock()
		procs = append(procs, proc)
		releases = append(releases, release)
		mu.Unlock()
		return proc, nil
	}

	cfg := Config{
		Project:    "testproject",
		PoolSize:   2,
		SpawnCmd:   "fake-agent",
		MaxRetries: 3,
		PromptDir:  testPromptDir(t),
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, progRunner(testTaskMeta), starter, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for first spawn.
	waitFor(t, func() bool {
		return spawnCount.Load() >= 1
	})

	// Crash the first agent (set error before release).
	mu.Lock()
	procs[0].err = fmt.Errorf("exit status 1")
	releases[0]()
	mu.Unlock()

	// Should respawn — wait for second spawn.
	waitFor(t, func() bool {
		return spawnCount.Load() >= 2
	})

	// Pool should still have one running agent (the respawned one).
	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	if got := spawnCount.Load(); got != 2 {
		t.Errorf("spawn count = %d, want 2", got)
	}
}

func TestCrashMaxRetriesExhausted(t *testing.T) {
	// With MaxRetries=2, the sequence is:
	//   initial spawn → crash → retries=1 (1 <= 2) → respawn
	//   respawn 1     → crash → retries=2 (2 <= 2) → respawn
	//   respawn 2     → crash → retries=3 (3 > 2)  → stop, max retries exhausted
	// Total spawns: 3 (initial + 2 retries).

	var spawnCount atomic.Int32
	var mu sync.Mutex
	procs := make([]*fakeProcess, 0)
	releases := make([]func(), 0)

	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		spawnCount.Add(1)
		proc, release := newFakeProcessWithError(int(spawnCount.Load())*100, fmt.Errorf("exit status 1"))
		mu.Lock()
		procs = append(procs, proc)
		releases = append(releases, release)
		mu.Unlock()
		return proc, nil
	}

	cfg := Config{
		Project:    "testproject",
		PoolSize:   2,
		SpawnCmd:   "fake-agent",
		MaxRetries: 2, // Allow 2 respawn attempts.
		PromptDir:  testPromptDir(t),
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, progRunner(testTaskMeta), starter, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for initial spawn.
	waitFor(t, func() bool {
		return spawnCount.Load() >= 1
	})

	// Crash agent 1 → retries=1 (<=2), should respawn.
	mu.Lock()
	releases[0]()
	mu.Unlock()

	waitFor(t, func() bool {
		return spawnCount.Load() >= 2
	})

	// Crash agent 2 → retries=2 (<=2), should respawn.
	mu.Lock()
	releases[1]()
	mu.Unlock()

	waitFor(t, func() bool {
		return spawnCount.Load() >= 3
	})

	// Crash agent 3 → retries=3 (>2), should NOT respawn.
	mu.Lock()
	releases[2]()
	mu.Unlock()

	// Wait for the reap to remove the agent from the pool.
	waitFor(t, func() bool {
		return len(pool.Status()) == 0
	})

	// Give extra time for any unexpected respawn.
	time.Sleep(50 * time.Millisecond)

	// Should have spawned exactly 3 times: initial + 2 retries.
	if got := spawnCount.Load(); got != 3 {
		t.Errorf("spawn count = %d, want 3 (initial + 2 retries)", got)
	}
}

func TestCrashCleanExitNoRespawn(t *testing.T) {
	var spawnCount atomic.Int32
	proc, release := newFakeProcess(1234) // Clean exit (no error).

	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		spawnCount.Add(1)
		return proc, nil
	}

	cfg := Config{
		Project:    "testproject",
		PoolSize:   2,
		SpawnCmd:   "fake-agent",
		MaxRetries: 3,
		PromptDir:  testPromptDir(t),
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, progRunner(testTaskMeta), starter, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return spawnCount.Load() >= 1
	})

	// Clean exit.
	release()

	waitFor(t, func() bool {
		return len(pool.Status()) == 0
	})

	time.Sleep(50 * time.Millisecond)

	// Should have spawned only once — no respawn on clean exit.
	if got := spawnCount.Load(); got != 1 {
		t.Errorf("spawn count = %d, want 1 (no respawn on clean exit)", got)
	}
}

func TestCrashRetryCountResetsOnSuccess(t *testing.T) {
	var spawnCount atomic.Int32
	var mu sync.Mutex
	procs := make([]*fakeProcess, 0)
	releases := make([]func(), 0)

	starter := func(ctx context.Context, spawnCmd string, prompt string) (Process, error) {
		spawnCount.Add(1)
		proc, release := newFakeProcess(int(spawnCount.Load()) * 100)
		mu.Lock()
		procs = append(procs, proc)
		releases = append(releases, release)
		mu.Unlock()
		return proc, nil
	}

	cfg := Config{
		Project:    "testproject",
		PoolSize:   2,
		SpawnCmd:   "fake-agent",
		MaxRetries: 2,
		PromptDir:  testPromptDir(t),
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, progRunner(testTaskMeta), starter, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for first spawn, crash it.
	waitFor(t, func() bool { return spawnCount.Load() >= 1 })
	mu.Lock()
	procs[0].err = fmt.Errorf("crash 1")
	releases[0]()
	mu.Unlock()

	// Wait for respawn.
	waitFor(t, func() bool { return spawnCount.Load() >= 2 })

	// This time exit cleanly — should reset retry count.
	mu.Lock()
	releases[1]() // Clean exit (no error set).
	mu.Unlock()

	waitFor(t, func() bool { return len(pool.Status()) == 0 })

	// Verify retry count was cleared.
	pool.mu.RLock()
	retries := pool.retries["ts-abc"]
	pool.mu.RUnlock()

	if retries != 0 {
		t.Errorf("retry count = %d, want 0 (should reset on clean exit)", retries)
	}
}

// --- helpers ---

// waitFor polls a condition with a timeout.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
