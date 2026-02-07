package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolModeDefaults(t *testing.T) {
	pool := testPool(t, progRunner(testTaskMeta), nil)
	if got := pool.Mode(); got != PoolActive {
		t.Errorf("default mode = %q, want %q", got, PoolActive)
	}
}

func TestPoolDrainStopsScheduling(t *testing.T) {
	var spawnCount atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		spawnCount.Add(1)
		proc, _ := newFakeProcess(int(spawnCount.Load()) * 100)
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 2)

	go pool.Run(ctx, taskCh)

	// Send first batch — should spawn.
	taskCh <- []Task{{ID: "ts-1", Priority: 1, Title: "First"}}
	waitFor(t, func() bool { return spawnCount.Load() >= 1 })

	// Drain — no more new scheduling.
	pool.Drain()

	if got := pool.Mode(); got != PoolDraining {
		t.Errorf("mode = %q, want %q", got, PoolDraining)
	}

	// Send second batch — should NOT spawn.
	taskCh <- []Task{{ID: "ts-2", Priority: 1, Title: "Second"}}
	time.Sleep(50 * time.Millisecond)

	if got := spawnCount.Load(); got != 1 {
		t.Errorf("spawn count = %d, want 1 (drain should block new scheduling)", got)
	}
}

func TestDrainAllowsCrashRespawn(t *testing.T) {
	var spawnCount atomic.Int32
	// Pre-allocate for exactly 2 spawns (initial + respawn after crash).
	// Indexed writes from goroutines are safe because each goroutine writes
	// to a distinct index (determined by the atomic spawnCount).
	procs := make([]*fakeProcess, 2)
	releases := make([]func(), 2)

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		n := spawnCount.Add(1)
		proc, release := newFakeProcess(int(n) * 100)
		idx := int(n) - 1
		procs[idx] = proc
		releases[idx] = release
		return proc, nil
	}

	cfg := Config{
		Project:    "testproject",
		PoolSize:   2,
		SpawnCmd:   "fake-agent",
		MaxRetries: 3,
		PromptDir:  "",
		LogDir:     t.TempDir(),
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, progRunner(testTaskMeta), starter, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for initial spawn.
	waitFor(t, func() bool { return spawnCount.Load() >= 1 })

	// Drain the pool.
	pool.Drain()

	// Crash the first agent — should respawn because drain allows respawns.
	procs[0].err = fmt.Errorf("exit status 1")
	releases[0]()

	waitFor(t, func() bool { return spawnCount.Load() >= 2 })

	// Pool should still have the respawned agent.
	waitFor(t, func() bool { return len(pool.Status()) == 1 })
}

func TestPauseStopsScheduling(t *testing.T) {
	var spawnCount atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		spawnCount.Add(1)
		proc, _ := newFakeProcess(int(spawnCount.Load()) * 100)
		return proc, nil
	}

	pool := testPool(t, progRunner(testTaskMeta), starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 2)

	go pool.Run(ctx, taskCh)

	// Send first batch — should spawn.
	taskCh <- []Task{{ID: "ts-1", Priority: 1, Title: "First"}}
	waitFor(t, func() bool { return spawnCount.Load() >= 1 })

	// Pause.
	pool.Pause()

	if got := pool.Mode(); got != PoolPaused {
		t.Errorf("mode = %q, want %q", got, PoolPaused)
	}

	// Send second batch — should NOT spawn.
	taskCh <- []Task{{ID: "ts-2", Priority: 1, Title: "Second"}}
	time.Sleep(50 * time.Millisecond)

	if got := spawnCount.Load(); got != 1 {
		t.Errorf("spawn count = %d, want 1 (pause should block new scheduling)", got)
	}
}

func TestPauseBlocksCrashRespawn(t *testing.T) {
	var spawnCount atomic.Int32
	proc, release := newFakeProcessWithError(1234, fmt.Errorf("exit status 1"))

	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		spawnCount.Add(1)
		return proc, nil
	}

	cfg := Config{
		Project:    "testproject",
		PoolSize:   2,
		SpawnCmd:   "fake-agent",
		MaxRetries: 3,
		PromptDir:  "",
		LogDir:     t.TempDir(),
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, progRunner(testTaskMeta), starter, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)

	// Wait for initial spawn.
	waitFor(t, func() bool { return spawnCount.Load() >= 1 })

	// Pause the pool.
	pool.Pause()

	// Crash the agent — should NOT respawn because pool is paused.
	release()

	// Wait for the agent to be reaped.
	waitFor(t, func() bool { return len(pool.Status()) == 0 })

	// Give time for any unexpected respawn.
	time.Sleep(50 * time.Millisecond)

	if got := spawnCount.Load(); got != 1 {
		t.Errorf("spawn count = %d, want 1 (pause should block respawns)", got)
	}
}

func TestResumeFromDrain(t *testing.T) {
	var spawnCount atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		spawnCount.Add(1)
		proc, _ := newFakeProcess(int(spawnCount.Load()) * 100)
		return proc, nil
	}

	// Use a runner that handles multiple tasks.
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

	pool := testPool(t, runner, starter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 3)

	go pool.Run(ctx, taskCh)

	// Drain first.
	pool.Drain()

	// Send tasks — should NOT spawn.
	taskCh <- []Task{{ID: "ts-1", Priority: 1, Title: "First"}}
	time.Sleep(50 * time.Millisecond)

	if got := spawnCount.Load(); got != 0 {
		t.Fatalf("spawn count = %d, want 0 during drain", got)
	}

	// Resume.
	pool.Resume()

	if got := pool.Mode(); got != PoolActive {
		t.Errorf("mode = %q, want %q", got, PoolActive)
	}

	// Send new tasks — should now spawn.
	taskCh <- []Task{{ID: "ts-2", Priority: 1, Title: "Second"}}
	waitFor(t, func() bool { return spawnCount.Load() >= 1 })
}

func TestResumeFromPause(t *testing.T) {
	pool := testPool(t, progRunner(testTaskMeta), nil)
	pool.ctx = context.Background()

	pool.Pause()
	if got := pool.Mode(); got != PoolPaused {
		t.Errorf("mode = %q, want %q", got, PoolPaused)
	}

	pool.Resume()
	if got := pool.Mode(); got != PoolActive {
		t.Errorf("mode = %q, want %q", got, PoolActive)
	}
}

func TestFullStatusIncludesPoolMode(t *testing.T) {
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
		LogDir:    t.TempDir(),
	}
	cfg.ApplyDefaults()

	runner := progRunnerWithShowAndReady(
		`{"title": "Task", "logs": []}`,
		"ID           PRI  TITLE\n",
	)
	pool := NewPool(cfg, runner, starter, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taskCh := make(chan []Task, 1)
	taskCh <- []Task{{ID: "ts-abc", Priority: 1, Title: "Do it"}}

	go pool.Run(ctx, taskCh)
	waitFor(t, func() bool { return len(pool.Status()) == 1 })

	// Active mode.
	status := BuildFullStatus(ctx, pool, cfg, runner)
	if status.PoolMode != PoolActive {
		t.Errorf("PoolMode = %q, want %q", status.PoolMode, PoolActive)
	}

	// Drain mode.
	pool.Drain()
	status = BuildFullStatus(ctx, pool, cfg, runner)
	if status.PoolMode != PoolDraining {
		t.Errorf("PoolMode = %q, want %q", status.PoolMode, PoolDraining)
	}

	// Paused mode.
	pool.Pause()
	status = BuildFullStatus(ctx, pool, cfg, runner)
	if status.PoolMode != PoolPaused {
		t.Errorf("PoolMode = %q, want %q", status.PoolMode, PoolPaused)
	}
}

// progRunnerWithShowAndReady returns a CommandRunner that handles
// prog start, prog show, and prog ready.
func progRunnerWithShowAndReady(showJSON, readyOutput string) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "start" {
			return []byte("Started"), nil
		}
		if len(args) >= 1 && args[0] == "show" {
			return []byte(showJSON), nil
		}
		if len(args) >= 1 && args[0] == "ready" {
			return []byte(readyOutput), nil
		}
		return nil, fmt.Errorf("unexpected command: %s %v", name, args)
	}
}
