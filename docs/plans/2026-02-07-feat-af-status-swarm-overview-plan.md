---
title: "feat: af status command — swarm overview with prog log summaries"
type: feat
date: 2026-02-07
task: ts-645f00
---

# af status command — swarm overview with prog log summaries

## Overview

Add a top-level `af status` CLI command that shows the swarm at a glance: which agents are running, what they're working on (sourced from prog log entries), and what's queued. One RPC call from CLI to daemon, daemon enriches pool data with prog metadata server-side.

## Problem Statement

The daemon runs agents but is a black box — the only way to see what's happening is `af daemon` which dumps raw JSON fields (socket, project, pool_size, agent count). There's no view of what each agent is actually doing, what task titles they're on, or what's waiting in the queue. Operators need a single command to assess swarm health.

## Proposed Solution

```
$ af status

Pool: 2/3 active

  blur_knife    ts-abc  12m  worker  "Fixed spawn(), 3 tests remain"
  sharp_proxy   ts-def   3m  worker  "All tests passing, running review"
  + 1 idle

Queue: 2 pending
  ts-ghi  P1  "Fix auth token expiry"
  ts-jkl  P2  "Refactor config loading"
```

**Activity summaries come from prog log entries.** Agents already write checkpoint messages via `prog log <task-id> "message"` during their work and at compaction handoffs. The last log entry is the most recent description of what the agent is doing. No JSONL parsing, no LLM summarization — just read what the agent told prog.

**Data sources per section:**
1. **Pool line** — `pool.Status()` length vs `config.PoolSize`
2. **Agent rows** — `pool.Status()` for id/task_id/role/spawn_time + `prog show <task-id> --json` for title + last log
3. **Idle line** — `config.PoolSize - len(agents)`
4. **Queue** — `prog ready -p <project>` parsed via existing `ParseProgReady`

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| RPC method | New `status.full` coexisting with `status` | `af daemon` keeps working, no breaking change |
| Command placement | Top-level `af status` | Most-used command should be shortest to type |
| Activity summary source | Last `prog log` entry per task | Already written by agents, semantic, zero new infra |
| Idle slot display | Single `+ N idle` line | Avoids N identical rows when pool is large |
| Partial failure handling | Return agents with empty summary, errors array | Never fail the whole request because one prog show failed |
| `prog show` calls | Parallel via errgroup, 5s timeout per call | Pool size 3 is fine sequential, but scale-ready |
| Agent ordering | By spawn time, oldest first | Stable ordering, most context at top |
| Output flags | `--json` and `--socket` from day 1 | Cheap now, expensive to retrofit |
| Colors | Plain text v1 | No deps, respect NO_COLOR. Color is a follow-up |
| Watch mode | Not in v1 | Separate ticket |
| Crashed agent history | Not in v1 | Need history tracking on pool. Separate ticket |

## Response Schema

New types in `internal/daemon/`:

```go
// status.go (new file)

// FullStatus is the response for the status.full RPC method.
type FullStatus struct {
    PoolSize int           `json:"pool_size"`
    Project  string        `json:"project"`
    Agents   []AgentStatus `json:"agents"`
    Queue    []QueuedTask  `json:"queue"`
    Errors   []string      `json:"errors,omitempty"`
}

// AgentStatus enriches Agent with task metadata from prog.
type AgentStatus struct {
    ID        string    `json:"id"`
    TaskID    string    `json:"task_id"`
    Role      string    `json:"role"`
    PID       int       `json:"pid"`
    SpawnTime time.Time `json:"spawn_time"`
    TaskTitle string    `json:"task_title"`
    LastLog   string    `json:"last_log,omitempty"`
}

// QueuedTask is a pending task from prog ready.
type QueuedTask struct {
    ID       string `json:"id"`
    Priority int    `json:"priority"`
    Title    string `json:"title"`
}
```

Note: `QueuedTask` mirrors the existing `Task` struct from `poll.go` but without the dependency. Could reuse `Task` directly — decide during implementation.

## Implementation Steps

### Step 1: Status types and enrichment logic (`internal/daemon/status.go`)

New file with:
- `FullStatus`, `AgentStatus`, `QueuedTask` types (schema above)
- `func BuildFullStatus(ctx context.Context, pool *Pool, cfg Config, runner CommandRunner) FullStatus`
  - Calls `pool.Status()` for running agents
  - Calls `prog show <task-id> --json` in parallel per agent via errgroup (5s timeout per call)
  - Parses title + last log entry from each `prog show` response
  - Calls `prog ready -p <project>` via existing `ParseProgReady`
  - Assembles and returns `FullStatus`
  - On per-agent failure: populate `AgentStatus` with what's known, append error to `Errors`
  - On queue failure: return empty queue, append error to `Errors`

**prog show response parsing:** Extend or duplicate `TaskMeta` to include title and logs:
```go
type taskShowResponse struct {
    Title string `json:"title"`
    Logs  []struct {
        Message   string `json:"message"`
        CreatedAt string `json:"created_at"`
    } `json:"logs"`
}
```

Only parse the fields we need (sparse deserialization, matching existing pattern in `role.go`).

### Step 2: Wire RPC method into daemon (`internal/daemon/daemon.go`)

- Add `case "status.full":` to `handleRequest` switch
- New handler `handleStatusFull` that calls `BuildFullStatus` and returns JSON
- Existing `"status"` method unchanged

### Step 3: Client method (`internal/client/client.go`)

- Add typed response struct mirroring `FullStatus` (or import from daemon package — decide based on dependency direction)
- New method `StatusFull() (*FullStatus, error)` that calls `"status.full"`
- Replace the untyped `map[string]any` pattern with proper struct deserialization

### Step 4: CLI command (`cmd/af/cmd/status.go`)

New file with:
- `var statusCmd = &cobra.Command{Use: "status", ...}`
- `--json` flag: output raw JSON response
- `--socket` flag: custom socket path (default: `/tmp/aetherd.sock`)
- Formatting logic for the human-readable table:
  - Pool header: `Pool: N/M active`
  - Agent rows: `name  task_id  uptime  role  "last_log or title"`
  - Idle line: `+ N idle` (only if idle > 0)
  - Queue section: `Queue: N pending` + task rows
- Error handling: daemon not running → `"daemon not running (start with: af daemon start --project <name>)"`
- Register in `init()`: `rootCmd.AddCommand(statusCmd)`

**Uptime formatting:** Compute `time.Since(agent.SpawnTime)` client-side from the ISO 8601 timestamp. Format as `3m`, `1h12m`, `2d3h` using a simple helper.

**Truncation:** For v1, no terminal width detection. Truncate task titles at 40 chars with `…`, log messages at 50 chars with `…`. These are reasonable for 80-col terminals.

### Step 5: Tests

**`internal/daemon/status_test.go`:**
- `TestBuildFullStatus` — happy path: 2 agents, prog show returns title + logs, prog ready returns 2 tasks
- `TestBuildFullStatusNoAgents` — empty pool, queue still populated
- `TestBuildFullStatusProgShowFails` — one prog show fails, agent still appears with empty summary, error in Errors
- `TestBuildFullStatusProgReadyFails` — queue fetch fails, agents still appear, error in Errors
- `TestBuildFullStatusNoLogs` — agent's task has no log entries, LastLog is empty

All tests use fake CommandRunner (matching existing pattern in `pool_test.go` `progRunner`).

**`cmd/af/cmd/status_test.go`:**
- Test the formatting logic in isolation (extract format function, test it with known inputs)

## Edge Cases

| Case | Behavior |
|---|---|
| Daemon not running | CLI prints error + hint to start daemon |
| Empty pool (no agents) | Show `Pool: 0/N active`, all idle, show queue |
| Agent just spawned (no logs) | Show agent row with title only, no summary quote |
| `prog show` fails for one agent | Agent row shows id/task_id/role/uptime, no title/summary. Error in Errors array |
| `prog ready` fails | Queue section shows error message instead of task list |
| Very long title/summary | Truncate with `…` at fixed widths |
| No project configured | Can't happen — `Config.Validate()` requires project |

## Files Changed

| File | Change |
|---|---|
| `internal/daemon/status.go` | **New** — FullStatus types, BuildFullStatus function |
| `internal/daemon/status_test.go` | **New** — tests for BuildFullStatus |
| `internal/daemon/daemon.go` | Add `status.full` case to handleRequest |
| `internal/client/client.go` | Add FullStatus struct, StatusFull() method |
| `cmd/af/cmd/status.go` | **New** — af status command + formatting |
| `cmd/af/cmd/status_test.go` | **New** — formatting tests |

## What This Does NOT Do

- No watch/follow mode (separate ticket)
- No crashed agent history (separate ticket — needs pool history tracking)
- No color/styling (separate ticket)
- No `af status <agent>` detail view (separate ticket — needs JSONL parsing)
- No stuck detection (separate epic)
- No intervention commands: kill, drain, pause (separate tickets under ep-0641d1)

## Verify

```bash
go test ./... -race -count=1
```

Manual: start daemon with `af daemon start -p aetherflow`, run `af status`, confirm output shows pool state. Run `af status --json`, confirm valid JSON. Without daemon running, confirm error message.
