---
module: daemon
date: 2026-02-07
problem_type: developer_experience
component: tooling
symptoms:
  - "no way to gracefully stop scheduling without killing the daemon"
  - "agent crash respawns cannot be selectively disabled"
  - "pool has only two states: running or stopped (no intermediate control)"
root_cause: missing_tooling
resolution_type: code_fix
severity: medium
tags:
  - pool-control
  - drain
  - pause
  - resume
  - flow-control
  - graceful-shutdown
  - operational-tooling
  - go
---

# Pool Flow Control: af drain, af pause, af resume

## Problem

The aetherflow daemon pool had no intermediate operational states between "fully active" and "daemon stopped." Operators couldn't gracefully wind down the pool (e.g., before a deploy) without killing the daemon, which also killed running agents. There was no way to stop crash respawns while keeping the daemon alive for status queries.

## Environment

- Module: daemon (pool scheduler)
- Go Version: 1.25.5
- Affected Component: `internal/daemon/pool.go`, CLI commands
- Date: 2026-02-07

## Symptoms

- No way to stop new task scheduling while letting current agents finish their work
- Agent crash respawns couldn't be disabled independently from scheduling
- Operators had to choose between "fully running" or "kill the daemon"

## What Didn't Work

**Direct solution:** The problem was identified and designed on the first attempt. The key design decision was distinguishing drain vs pause semantics for crash respawns.

## Solution

Added a `PoolMode` state machine with three modes:

| Mode | New Scheduling | Crash Respawns | Use Case |
|------|---------------|----------------|----------|
| `active` (default) | ✅ | ✅ | Normal operation |
| `draining` | ❌ | ✅ | Pre-deploy: let work finish, respawn crashes since task is already claimed |
| `paused` | ❌ | ❌ | Emergency: stop everything, investigate |

### Pool mode field and methods

```go
// pool.go — PoolMode type and constants
type PoolMode string

const (
    PoolActive   PoolMode = "active"
    PoolDraining PoolMode = "draining"
    PoolPaused   PoolMode = "paused"
)

// Mode transitions log from/to for auditability
func (p *Pool) Drain() {
    p.mu.Lock()
    defer p.mu.Unlock()
    prev := p.mode
    p.mode = PoolDraining
    p.log.Info("pool mode changed", "from", prev, "to", PoolDraining)
}
```

### Schedule guard

```go
// pool.go — schedule() early return when not active
func (p *Pool) schedule(ctx context.Context, tasks []Task) {
    p.mu.RLock()
    mode := p.mode
    p.mu.RUnlock()

    if mode != PoolActive {
        p.log.Debug("schedule skipped, pool not active", "mode", mode, "task_count", len(tasks))
        return
    }
    // ... normal scheduling continues
}
```

### Respawn guard (drain vs pause distinction)

```go
// pool.go — respawn() only blocks when paused, not draining
func (p *Pool) respawn(taskID string, role Role) {
    p.mu.RLock()
    mode := p.mode
    p.mu.RUnlock()

    if mode == PoolPaused {
        p.log.Info("respawn skipped, pool is paused", "task_id", taskID, "role", role)
        return
    }
    // ... respawn continues (allowed in active AND draining)
}
```

### RPC handlers

```go
// pool_control.go — thin RPC handlers with nil-pool guard
func (d *Daemon) handlePoolDrain() *Response {
    if d.pool == nil {
        return &Response{Success: false, Error: "no pool configured"}
    }
    d.pool.Drain()
    return d.poolModeResponse()
}

// poolModeResponse builds response with mode and running count.
// Callers must ensure d.pool is non-nil before calling.
func (d *Daemon) poolModeResponse() *Response {
    result, err := json.Marshal(PoolModeResult{
        Mode:    d.pool.Mode(),
        Running: len(d.pool.Status()),
    })
    if err != nil {
        return &Response{Success: false, Error: fmt.Sprintf("marshal pool mode: %v", err)}
    }
    return &Response{Success: true, Result: result}
}
```

### CLI commands

```go
// pool_control.go (cmd) — uses server-reported mode, not hardcoded strings
func printPoolModeResult(result *client.PoolModeResult) {
    fmt.Printf("pool %s (%d agents running)\n", result.Mode, result.Running)
}
```

### Status output integration

```go
// status.go — shows pool mode in af status output
if status.PoolMode != "" && status.PoolMode != "active" {
    fmt.Printf(" [%s]", status.PoolMode)
}
```

## Why This Works

The design separates **scheduling** (assigning new tasks from the queue) from **respawning** (restarting a crashed agent on an already-claimed task). This distinction matters because:

1. **Draining** — The task is already claimed in prog (`prog start` was called). Killing the agent without respawn would orphan the task in "in_progress" state with no agent working on it. So draining allows respawns to keep the invariant: every in_progress task has an agent.

2. **Paused** — A stronger stop. No respawns because the operator wants full control, possibly to investigate a crash loop or bad state. The operator accepts that in_progress tasks may be orphaned until resume.

3. **Locking** — `PoolMode` is a string field protected by the pool's existing `sync.RWMutex`. Mode reads use `RLock`, writes use `Lock`. No new locks or atomics needed.

4. **Resume** — Returns to active mode. Tasks dropped during drain/pause are not retroactively scheduled; the next poll cycle picks them up naturally.

## Prevention

- **Always log mode transitions with from/to** — makes it auditable when debugging "why didn't my task schedule?"
- **Log when schedule() skips** — debug-level log with mode and task count so operators can trace why tasks aren't being assigned
- **Use server-reported mode in CLI** — don't hardcode strings; use `result.Mode` from the server response so the CLI always reflects actual server state
- **Guard nil pool in handlers, not in shared helpers** — each handler checks `d.pool == nil` before calling `poolModeResponse()`, keeping the helper simpler

## Review Findings Applied

A 3-agent review (code-reviewer, simplicity-reviewer, grug-brain) found:

1. **Test race condition** — `TestDrainAllowsCrashRespawn` used `append` from goroutines on shared slices. Fixed by pre-allocating to known size and using indexed writes.
2. **Missing LogDir in tests** — Two tests constructed `Config` without `LogDir: t.TempDir()`, leaking files to real filesystem.
3. **Hardcoded CLI strings** — `printPoolModeResult` took a hardcoded action string instead of using the server-reported `result.Mode`.
4. **Redundant nil check** — `poolModeResponse()` checked `d.pool == nil` but every caller already checked. Removed and documented the precondition.
5. **Silent schedule skip** — `schedule()` returned silently when mode wasn't active. Added debug log with mode and task count.

## Related Issues

- See also: [af-status-watch-mode.md](./af-status-watch-mode.md) — status output now shows `[draining]`/`[paused]` mode indicator
- See also: [af-logs-tail-agent-jsonl-20260207.md](./af-logs-tail-agent-jsonl-20260207.md) — log tailing for monitoring agents during drain
- See also: [post-review-hardening-findings-daemon-20260207.md](../best-practices/post-review-hardening-findings-daemon-20260207.md) — multi-agent review pattern used here
