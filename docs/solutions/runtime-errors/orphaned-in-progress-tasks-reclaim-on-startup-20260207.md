---
module: daemon
date: 2026-02-07
problem_type: runtime_error
component: tooling
symptoms:
  - "Tasks stuck in in_progress after daemon crash with no running agent"
  - "prog ready only returns open tasks, so orphaned in_progress tasks are invisible to polling"
  - "Manual sqlite3 UPDATE required to unstick tasks after daemon restart"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - orphaned-tasks
  - daemon-crash
  - reclaim
  - prog
  - pool
  - data-race
  - context
  - go
---

# Orphaned In-Progress Tasks: Reclaim on Daemon Startup

## Problem

When the daemon crashes or is stopped while agents are running, tasks stay `in_progress` in prog but have no process. On restart, `prog ready` only returns `open` tasks, so these orphaned tasks are permanently stuck — no agent picks them up and no error is reported.

## Environment

- Module: daemon (internal/daemon)
- Go Version: 1.25.5
- Affected Component: Pool scheduling, daemon startup, prog integration
- Date: 2026-02-07

## Symptoms

- After daemon crash/restart, `prog list --status in_progress --type task` shows tasks with no corresponding agent
- `af status` shows fewer agents than expected — orphaned tasks are invisible
- Only fix was manual database intervention:
  ```sql
  UPDATE items SET status = 'open', updated_at = CURRENT_TIMESTAMP
  WHERE id IN ('ts-406d55', 'ts-c97c4d');
  ```
- The `prog` CLI has no `reopen` command, so the DB update was the only path

## What Didn't Work

**Direct solution:** The problem was identified on first encounter when two eldspire-hexmap tasks got stuck after a daemon stop. The manual DB fix was applied immediately, then the reclaim feature was built to prevent future occurrences.

## Solution

Added a `Reclaim` method to `Pool` that runs once at daemon startup, queries prog for in_progress tasks, and respawns agents for any that aren't already running.

**New file: `internal/daemon/reclaim.go`**

```go
// fetchInProgressTasks queries prog for tasks currently in_progress.
func fetchInProgressTasks(ctx context.Context, project string, runner CommandRunner) ([]Task, error) {
    output, err := runner(ctx, "prog", "list", "--status", "in_progress",
        "--type", "task", "--json", "-p", project)
    // ... parse JSON into []Task
}

// Reclaim spawns agents for orphaned in_progress tasks.
func (p *Pool) Reclaim(ctx context.Context) {
    // Skip if paused — operator intentionally stopped work
    // Query prog for in_progress tasks
    // For each: skip if already running, skip if pool full
    // Infer role from task metadata, then respawn
}
```

**Wiring in `daemon.go`:**

```go
// Set pool context before launching goroutines so both Run and
// Reclaim (which calls respawn, which uses p.ctx) are safe.
d.pool.SetContext(ctx)

taskCh := d.poller.Start(ctx)
go d.pool.Run(ctx, taskCh)
go d.pool.Reclaim(ctx)
```

**Data race fix in `pool.go`:**

The `respawn` function uses `p.ctx` for process lifecycle management. With `Run` and `Reclaim` starting concurrently, both could write `p.ctx`. Fixed by:

1. Adding `SetContext(ctx)` — called once before either goroutine starts
2. `Run()` only sets `p.ctx` if it wasn't already set (nil guard for backward compat with tests):

```go
func (p *Pool) Run(ctx context.Context, taskCh <-chan []Task) {
    // If SetContext wasn't called (standalone usage, tests), set it now.
    if p.ctx == nil {
        p.ctx = ctx
    }
    // ...
}
```

**Reclaim behaviors:**
- Skips tasks that already have a running agent in the pool
- Respects pool size limits (stops when full, logs deferred count)
- Skips when pool is paused (operator intentionally stopped work)
- Handles partial metadata failures (one task fails, others still reclaim)
- Uses `respawn` path (not `spawn`) since tasks are already in_progress in prog

## Why This Works

The fundamental issue is that prog's task state and the daemon's process state can diverge on crash. Prog persists `in_progress` to disk, but the daemon's agent processes are ephemeral. On restart, the daemon has a clean pool but prog still shows tasks as claimed.

Reclaim bridges this gap by:
1. Querying prog's persisted state for in_progress tasks
2. Comparing against the pool's running agents (empty on fresh start)
3. Respawning agents for the difference

Using the `respawn` path (not `spawn`) is key — `spawn` would call `prog start` which transitions `open → in_progress`, but these tasks are already in_progress. `respawn` skips the state transition and just starts the agent process.

The data race fix ensures `p.ctx` is set exactly once before any goroutine reads it. The nil guard in `Run()` preserves backward compatibility — tests that call `Run()` directly without `SetContext()` still work because `Run` falls back to setting it.

## Prevention

- **Assume crash recovery is needed.** Any system with persistent external state (prog) and ephemeral internal state (processes) needs reconciliation on startup. Design for crash recovery from the start.
- **Don't share mutable state between concurrent goroutines without synchronization.** The `p.ctx` field was written by both `Run` and `SetContext` from different goroutines. Fix: write once before goroutines start, guard subsequent writes.
- **Test with the race detector.** `go test -race ./...` catches data races that compile and run correctly most of the time but fail under load.
- **Separate "already claimed" from "needs claiming" paths.** The pool had `spawn` (claim + start) but lacked a "just start" path for already-claimed tasks. `respawn` served this purpose but was only wired for crash-and-retry, not startup reconciliation.

## Related Issues

- See also: [nil-pointer-status-handler](./nil-pointer-status-handler-runner-not-set-20260207.md) — another daemon startup issue where Config fields weren't initialized
- See also: [daemon-fails-outside-repo-root](./daemon-fails-outside-repo-root-embed-prompts-20260207.md) — daemon startup reliability fix from same session
- See also: [daemon-cross-project-shutdown-socket-isolation](../security-issues/daemon-cross-project-shutdown-socket-isolation-20260207.md) — cross-project socket isolation and path traversal hardening
