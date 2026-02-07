package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
)

func TestFetchInProgressTasks(t *testing.T) {
	items := []progListItem{
		{ID: "ts-abc", Title: "Task A", Type: "task", Status: "in_progress"},
		{ID: "ts-def", Title: "Task B", Type: "task", Status: "in_progress"},
	}
	data, _ := json.Marshal(items)

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return data, nil
	}

	tasks, err := fetchInProgressTasks(context.Background(), "testproject", runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].ID != "ts-abc" {
		t.Errorf("tasks[0].ID = %q, want %q", tasks[0].ID, "ts-abc")
	}
	if tasks[1].Title != "Task B" {
		t.Errorf("tasks[1].Title = %q, want %q", tasks[1].Title, "Task B")
	}
}

func TestFetchInProgressTasksEmpty(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("[]"), nil
	}

	tasks, err := fetchInProgressTasks(context.Background(), "testproject", runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("got %d tasks, want 0", len(tasks))
	}
}

func TestFetchInProgressTasksCommandError(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("error output"), fmt.Errorf("exit status 1")
	}

	_, err := fetchInProgressTasks(context.Background(), "testproject", runner)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestReclaimSpawnsOrphanedTasks(t *testing.T) {
	var spawnCount atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		n := spawnCount.Add(1)
		proc, _ := newFakeProcess(int(n) * 100)
		return proc, nil
	}

	// Runner that handles both prog list (for reclaim) and prog show (for role inference).
	orphanedTasks := []progListItem{
		{ID: "ts-orphan1", Title: "Orphan 1", Type: "task", Status: "in_progress"},
		{ID: "ts-orphan2", Title: "Orphan 2", Type: "task", Status: "in_progress"},
	}
	orphanedJSON, _ := json.Marshal(orphanedTasks)

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "list" {
			return orphanedJSON, nil
		}
		if len(args) >= 1 && args[0] == "show" {
			// Return task metadata for role inference.
			taskID := args[1]
			meta := fmt.Sprintf(`{"id":"%s","type":"task","definition_of_done":"Do it","labels":[]}`, taskID)
			return []byte(meta), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}

	cfg := Config{
		Project:  "testproject",
		PoolSize: 3,
		SpawnCmd: "fake-agent",
		LogDir:   t.TempDir(),
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, runner, starter, slog.Default())
	pool.SetContext(context.Background())

	pool.Reclaim(context.Background())

	// Wait for spawns to complete.
	waitFor(t, func() bool {
		return len(pool.Status()) == 2
	})

	if got := spawnCount.Load(); got != 2 {
		t.Errorf("spawn count = %d, want 2", got)
	}

	// Verify both agents are in the pool.
	agents := pool.Status()
	taskIDs := map[string]bool{}
	for _, a := range agents {
		taskIDs[a.TaskID] = true
	}
	if !taskIDs["ts-orphan1"] || !taskIDs["ts-orphan2"] {
		t.Errorf("expected both orphaned tasks in pool, got: %v", taskIDs)
	}
}

func TestReclaimSkipsAlreadyRunning(t *testing.T) {
	proc, release := newFakeProcess(1234)
	defer release()

	var spawnCount atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		spawnCount.Add(1)
		return proc, nil
	}

	// One orphaned task that's already in the pool.
	orphanedTasks := []progListItem{
		{ID: "ts-already", Title: "Already Running", Type: "task", Status: "in_progress"},
	}
	orphanedJSON, _ := json.Marshal(orphanedTasks)

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "list" {
			return orphanedJSON, nil
		}
		if len(args) >= 1 && args[0] == "show" {
			return []byte(`{"id":"ts-already","type":"task","definition_of_done":"Do it","labels":[]}`), nil
		}
		if len(args) >= 1 && args[0] == "start" {
			return []byte("Started"), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}

	pool := testPool(t, runner, starter)
	pool.SetContext(context.Background())

	// Pre-populate the pool with the "already running" task.
	pool.mu.Lock()
	pool.agents["ts-already"] = &Agent{
		ID:     "existing_agent",
		TaskID: "ts-already",
		State:  AgentRunning,
	}
	pool.mu.Unlock()

	pool.Reclaim(context.Background())

	// Should not have spawned — task is already running.
	if got := spawnCount.Load(); got != 0 {
		t.Errorf("spawn count = %d, want 0 (task already running)", got)
	}
}

func TestReclaimRespectsPoolSize(t *testing.T) {
	var spawnCount atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		n := spawnCount.Add(1)
		proc, _ := newFakeProcess(int(n) * 100)
		return proc, nil
	}

	// 3 orphaned tasks but pool size is 2.
	orphanedTasks := []progListItem{
		{ID: "ts-1", Title: "Task 1", Type: "task", Status: "in_progress"},
		{ID: "ts-2", Title: "Task 2", Type: "task", Status: "in_progress"},
		{ID: "ts-3", Title: "Task 3", Type: "task", Status: "in_progress"},
	}
	orphanedJSON, _ := json.Marshal(orphanedTasks)

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "list" {
			return orphanedJSON, nil
		}
		if len(args) >= 2 && args[0] == "show" {
			meta := fmt.Sprintf(`{"id":"%s","type":"task","definition_of_done":"Do it","labels":[]}`, args[1])
			return []byte(meta), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}

	cfg := Config{
		Project:  "testproject",
		PoolSize: 2, // Only 2 slots.
		SpawnCmd: "fake-agent",
		LogDir:   t.TempDir(),
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, runner, starter, slog.Default())
	pool.SetContext(context.Background())

	pool.Reclaim(context.Background())

	// Wait for spawns.
	waitFor(t, func() bool {
		return len(pool.Status()) == 2
	})

	// Should only have spawned 2 (pool full).
	if got := spawnCount.Load(); got != 2 {
		t.Errorf("spawn count = %d, want 2 (pool size limit)", got)
	}
}

func TestReclaimPartialMetadataFailure(t *testing.T) {
	var spawnCount atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		n := spawnCount.Add(1)
		proc, _ := newFakeProcess(int(n) * 100)
		return proc, nil
	}

	orphanedTasks := []progListItem{
		{ID: "ts-good", Title: "Good Task", Type: "task", Status: "in_progress"},
		{ID: "ts-bad", Title: "Bad Task", Type: "task", Status: "in_progress"},
		{ID: "ts-also-good", Title: "Also Good", Type: "task", Status: "in_progress"},
	}
	orphanedJSON, _ := json.Marshal(orphanedTasks)

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "list" {
			return orphanedJSON, nil
		}
		if len(args) >= 2 && args[0] == "show" {
			taskID := args[1]
			if taskID == "ts-bad" {
				return nil, fmt.Errorf("prog show: task not found")
			}
			meta := fmt.Sprintf(`{"id":"%s","type":"task","definition_of_done":"Do it","labels":[]}`, taskID)
			return []byte(meta), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}

	cfg := Config{
		Project:  "testproject",
		PoolSize: 5,
		SpawnCmd: "fake-agent",
		LogDir:   t.TempDir(),
	}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, runner, starter, slog.Default())
	pool.SetContext(context.Background())

	pool.Reclaim(context.Background())

	// Wait for the 2 good tasks to spawn.
	waitFor(t, func() bool {
		return len(pool.Status()) == 2
	})

	if got := spawnCount.Load(); got != 2 {
		t.Errorf("spawn count = %d, want 2 (ts-bad should have been skipped)", got)
	}

	agents := pool.Status()
	taskIDs := map[string]bool{}
	for _, a := range agents {
		taskIDs[a.TaskID] = true
	}
	if !taskIDs["ts-good"] || !taskIDs["ts-also-good"] {
		t.Errorf("expected ts-good and ts-also-good in pool, got: %v", taskIDs)
	}
	if taskIDs["ts-bad"] {
		t.Errorf("ts-bad should not have been reclaimed, got: %v", taskIDs)
	}
}

func TestReclaimSkipsWhenPaused(t *testing.T) {
	var spawnCount atomic.Int32
	starter := func(ctx context.Context, spawnCmd string, prompt string, _ string, _ io.Writer) (Process, error) {
		spawnCount.Add(1)
		proc, _ := newFakeProcess(999)
		return proc, nil
	}

	orphanedTasks := []progListItem{
		{ID: "ts-orphan", Title: "Orphan", Type: "task", Status: "in_progress"},
	}
	orphanedJSON, _ := json.Marshal(orphanedTasks)

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "list" {
			return orphanedJSON, nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}

	pool := testPool(t, runner, starter)
	pool.SetContext(context.Background())

	// Pause the pool before reclaiming.
	pool.mu.Lock()
	pool.mode = PoolPaused
	pool.mu.Unlock()

	pool.Reclaim(context.Background())

	// Should not have spawned — pool is paused.
	if got := spawnCount.Load(); got != 0 {
		t.Errorf("spawn count = %d, want 0 (pool is paused)", got)
	}
}

func TestReclaimNoOrphans(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "list" {
			return []byte("[]"), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}

	pool := testPool(t, runner, nil)
	pool.SetContext(context.Background())

	// Should not panic or error.
	pool.Reclaim(context.Background())

	if got := len(pool.Status()); got != 0 {
		t.Errorf("pool should be empty, got %d agents", got)
	}
}
