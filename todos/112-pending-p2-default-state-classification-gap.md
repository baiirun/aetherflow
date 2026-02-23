---
status: pending
priority: p2
issue_id: "112"
tags: [code-review, correctness, display]
dependencies: []
---

# Fix default state classification in remote spawn display

## Problem Statement

In `cmd/af/cmd/status.go`, the state-bucketing switch for remote spawn summary counts falls through to `default: running++` for any state not matched by `IsRemoteSpawnTerminal` or `IsRemoteSpawnPending`. This means a typo like `"runnning"` or a new state `"pausing"` would silently count as "running" — the most misleading possible default.

Flagged by code-reviewer and tigerstyle-reviewer as a correctness gap violating the "assert the negative space" principle.

## Findings

- `cmd/af/cmd/status.go:271-278` — state bucketing switch
- `cmd/af/cmd/status.go:311-318` — color selection switch (same gap)
- There's no `IsRemoteSpawnRunning` helper in the client package
- The `default` case in both switches silently absorbs unknown states as "running"
- The state set is currently exhaustive (6 states), but the gap is latent

## Proposed Solutions

### Option 1: Add explicit `IsRemoteSpawnRunning` check with graceful default

**Approach:** Add `IsRemoteSpawnRunning(state string) bool` to client. Use it in the explicit case. Keep `default` as a graceful fallback to pending (safer than running).

**Pros:**
- Asserts the positive space for all three categories
- Unknown states degrade to pending (safer than running)
- Clear code intent

**Cons:**
- One more helper function

**Effort:** 15 minutes

**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/client/client.go` — add `IsRemoteSpawnRunning` helper
- `cmd/af/cmd/status.go:271-278` — state bucketing switch
- `cmd/af/cmd/status.go:311-318` — color selection switch

## Acceptance Criteria

- [ ] `IsRemoteSpawnRunning` helper exists in client package
- [ ] Both switch statements explicitly check for running state
- [ ] Default case degrades to pending (not running)
- [ ] Tests pass

## Work Log

### 2026-02-22 - Initial Discovery

**By:** Claude Code (code review)

**Actions:**
- Identified gap in state classification switches
- Confirmed state set is currently exhaustive but gap is latent
- Recommended explicit running check with safer default
