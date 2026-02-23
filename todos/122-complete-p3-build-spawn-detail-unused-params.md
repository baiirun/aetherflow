---
status: pending
priority: p3
issue_id: "122"
tags: [code-review, simplification, api-design]
dependencies: ["119"]
---

# Remove unused context and config params from buildSpawnDetail

## Problem Statement

`buildSpawnDetail` accepts `context.Context` and `Config` parameters that are blanked with `_`. These were carried over from the pool-agent path but spawns don't call prog, so the parameters are never used. The signature implies these are needed when they aren't.

Note: This may be resolved as a side effect of #119 (BuildAgentDetail param refactor).

## Findings

- `internal/daemon/status.go:316` — `func buildSpawnDetail(_ context.Context, entry *SpawnEntry, events *EventBuffer, _ Config, params StatusAgentParams)`
- Both `context.Context` and `Config` are blanked
- `buildRemoteSpawnDetail` (line 351) correctly omits these params

## Proposed Solutions

### Option 1: Remove unused parameters

**Approach:** Remove `ctx` and `cfg` from the signature, update the single call site (line 252).

**Effort:** 5 minutes
**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status.go:316` — function signature
- `internal/daemon/status.go:252` — call site

## Acceptance Criteria

- [ ] No blanked `_` parameters in `buildSpawnDetail`
- [ ] Tests pass

## Work Log

### 2026-02-22 - Code Review Round 5

**By:** Claude Code

**Actions:**
- Identified unused parameters during simplicity review
