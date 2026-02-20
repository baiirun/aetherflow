package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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

// testPool creates a pool with sensible test defaults and the given fakes.
// PromptDir is empty so the pool uses embedded prompts compiled into the binary.
func testPool(t *testing.T, runner CommandRunner, starter ProcessStarter) *Pool {
	t.Helper()
	cfg := Config{
		Project:  "testproject",
		PoolSize: 2,
		SpawnCmd: "fake-agent",
		// PromptDir empty — uses embedded prompts.
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
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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

func TestPoolStatus(t *testing.T) {
	proc, release := newFakeProcess(1234)
	defer release()

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		spawnCount.Add(1)
		return proc, nil
	}

	cfg := Config{
		Project:    "testproject",
		PoolSize:   2,
		SpawnCmd:   "fake-agent",
		MaxRetries: 3,
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

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
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

func TestSpawnFailsGracefullyOnStarterError(t *testing.T) {
	var attempted atomic.Bool
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		attempted.Store(true)
		return nil, fmt.Errorf("spawn failed")
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for the spawn attempt to complete.
	waitFor(t, func() bool {
		return attempted.Load()
	})

	// Pool should have no agents after a failed spawn.
	if got := len(pool.Status()); got != 0 {
		t.Errorf("pool should have 0 agents after starter failure, got %d", got)
	}
}

// --- AETHERFLOW_AGENT_ID env var tests ---

func TestExecProcessStarterSetsAgentIDEnv(t *testing.T) {
	// Spawn a real process that prints the AETHERFLOW_AGENT_ID env var to stdout.
	// We use "sh -c" as the spawnCmd so the prompt becomes the shell script.
	var buf strings.Builder
	proc, err := ExecProcessStarter(
		context.Background(),
		"sh -c",                        // spawnCmd
		"printenv AETHERFLOW_AGENT_ID", // prompt (becomes the shell command)
		"steel_gloom",                  // agentID
		&buf,                           // stdout
	)
	if err != nil {
		t.Fatalf("ExecProcessStarter: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("process exited with error: %v", err)
	}

	got := strings.TrimSpace(buf.String())
	if got != "steel_gloom" {
		t.Errorf("AETHERFLOW_AGENT_ID = %q, want %q", got, "steel_gloom")
	}
}

func TestSpawnPassesAgentIDToStarter(t *testing.T) {
	proc, release := newFakeProcess(1234)
	defer release()

	var gotAgentID string
	starter := func(ctx context.Context, spawnCmd string, prompt string, agentID string, _ io.Writer) (Process, error) {
		gotAgentID = agentID
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

	// The agent ID should be non-empty (generated by NameGenerator).
	if gotAgentID == "" {
		t.Error("agentID passed to starter was empty, want non-empty name")
	}

	// Verify the agent ID in the pool matches what was passed to the starter.
	agents := pool.Status()
	if string(agents[0].ID) != gotAgentID {
		t.Errorf("agent ID mismatch: pool has %q, starter got %q", agents[0].ID, gotAgentID)
	}
}

// --- dead agent sweep tests ---

func TestPoolSweepsDeadProcess(t *testing.T) {
	// Simulate an agent whose Wait() hangs forever but whose PID is gone.
	// sweepDead should remove it from the pool.
	proc := &fakeProcess{pid: 99999, waitCh: make(chan struct{})} // never closed → Wait blocks forever

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)
	// Override pidAlive to report the PID as dead.
	pool.pidAlive = func(pid int) bool { return false }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for agent to appear.
	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	// Manually trigger sweep (don't wait 30s for the ticker).
	pool.sweepDead()

	// Agent should be removed.
	if got := len(pool.Status()); got != 0 {
		t.Errorf("after sweep: running agents = %d, want 0", got)
	}
}

func TestPoolSweepKeepsAliveProcess(t *testing.T) {
	// Agent with a live PID should not be removed by sweep.
	proc, release := newFakeProcess(1234)
	defer release()

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)
	// Override pidAlive to report the PID as alive.
	pool.pidAlive = func(pid int) bool { return true }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	// Sweep should not remove a live agent.
	pool.sweepDead()

	if got := len(pool.Status()); got != 1 {
		t.Errorf("after sweep: running agents = %d, want 1", got)
	}
}

func TestPoolSweepFreesSlot(t *testing.T) {
	// After sweeping a dead agent, the freed slot should allow a new task
	// to be scheduled on the next poll cycle.
	var spawnCount atomic.Int32

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		spawnCount.Add(1)
		// All processes block forever (simulating hung Wait).
		proc := &fakeProcess{pid: int(spawnCount.Load()) * 100, waitCh: make(chan struct{})}
		return proc, nil
	}

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
	// All PIDs report dead on sweep.
	pool.pidAlive = func(pid int) bool { return false }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 2)
	// Fill both slots.
	taskCh <- []Task{
		{ID: "ts-1", Priority: 1, Title: "First"},
		{ID: "ts-2", Priority: 1, Title: "Second"},
	}

	go pool.Run(ctx, taskCh)

	waitFor(t, func() bool {
		return len(pool.Status()) == 2
	})

	// Sweep kills both.
	pool.sweepDead()

	if got := len(pool.Status()); got != 0 {
		t.Fatalf("after sweep: running agents = %d, want 0", got)
	}

	// Now a new task should be schedulable.
	taskCh <- []Task{{ID: "ts-3", Priority: 1, Title: "Third"}}

	waitFor(t, func() bool {
		return len(pool.Status()) == 1
	})

	agents := pool.Status()
	if agents[0].TaskID != "ts-3" {
		t.Errorf("new agent task = %q, want %q", agents[0].TaskID, "ts-3")
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
