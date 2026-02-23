---
status: pending
priority: p3
issue_id: "097"
tags: [code-review, correctness]
dependencies: []
---

# Reconcile plan vs code: `running → unknown` state transition

## Problem Statement

The plan says `running` can transition to `unknown` (line 173: `running → terminated, unknown`), but the code's `validRemoteSpawnTransitions` map only allows `running → {failed, terminated}`. Either the plan is stale or the code is missing a transition.

## Findings

- `docs/plans/...md:173` — `running | ... | terminated, unknown`
- `internal/daemon/remote_spawn_store.go:377` — `RemoteSpawnRunning: {RemoteSpawnFailed, RemoteSpawnTerminated}`
- `unknown` represents indeterminate outcomes (partial failure, timeout, crash) — these could happen from `running`

## Proposed Solutions

### Option 1: Add `unknown` to running transitions

**Approach:** Update `validRemoteSpawnTransitions` to include `RemoteSpawnUnknown` in `RemoteSpawnRunning`'s transitions.

### Option 2: Update the plan

**Approach:** Remove `unknown` from `running`'s transitions in the plan if it's intentionally excluded.

**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Plan and code agree on `running` state transitions

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified discrepancy between plan and implementation
