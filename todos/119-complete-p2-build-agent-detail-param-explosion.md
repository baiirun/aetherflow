---
status: pending
priority: p2
issue_id: "119"
tags: [code-review, architecture, api-design]
dependencies: []
---

# Reduce BuildAgentDetail to struct params — match StatusSources pattern

## Problem Statement

`BuildAgentDetail` has 8 positional parameters, including 3 nilable pointer types in a row (`*SpawnRegistry`, `*RemoteSpawnStore`, `*EventBuffer`). Call sites like `BuildAgentDetail(ctx, pool, nil, nil, events, cfg, runner, params)` are error-prone — swapping `d.spawns` and `d.rspawns` compiles silently since both are pointer types. `BuildFullStatus` already solved this problem with `StatusSources`.

## Findings

- `internal/daemon/status.go:236` — 8-param function signature
- `internal/daemon/daemon.go:339` — call site with all 8 params inline
- `internal/daemon/status_agent_test.go` — 9 test call sites, many with `nil, nil, nil` sequences
- `internal/daemon/status.go:77-81` — `StatusSources` struct exists and could be extended
- Flagged by: code-reviewer, grug-brain-reviewer, architecture-strategist, tigerstyle-reviewer, code-simplicity-reviewer (5/10 agents)

## Proposed Solutions

### Option 1: Extend StatusSources with Events field

**Approach:** Add `Events *EventBuffer` to `StatusSources`, then refactor `BuildAgentDetail` to accept `StatusSources` instead of 4 separate pointer params.

```go
type StatusSources struct {
    Pool         *Pool
    Spawns       *SpawnRegistry
    RemoteSpawns *RemoteSpawnStore
    Events       *EventBuffer
}

func BuildAgentDetail(ctx context.Context, src StatusSources, cfg Config, runner CommandRunner, params StatusAgentParams) (*AgentDetail, error)
```

**Pros:**
- Self-documenting call sites
- Reuses existing pattern
- 5 params instead of 8

**Cons:**
- `BuildFullStatus` doesn't use `Events`, so the struct carries a field irrelevant to one consumer

**Effort:** 30 minutes

**Risk:** Low

---

### Option 2: Separate AgentDetailSources struct

**Approach:** Create a dedicated `AgentDetailSources` struct for `BuildAgentDetail` that includes all 5 data sources (pool, spawns, rspawns, events, runner).

**Pros:**
- Each function gets exactly the struct it needs
- No unused fields

**Cons:**
- Two similar structs to maintain
- Runner is already in Config via `cfg.Runner`

**Effort:** 30 minutes

**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status.go` — `StatusSources` struct, `BuildAgentDetail` signature
- `internal/daemon/daemon.go:339` — call site
- `internal/daemon/status_agent_test.go` — 9 test call sites

## Acceptance Criteria

- [ ] `BuildAgentDetail` takes ≤ 5 parameters
- [ ] Call sites use named struct fields (self-documenting)
- [ ] All existing tests pass
- [ ] `go build ./...` passes

## Work Log

### 2026-02-22 - Code Review Round 5

**By:** Claude Code

**Actions:**
- Identified parameter explosion during 10-agent parallel review
- 5 of 10 agents independently flagged this finding
