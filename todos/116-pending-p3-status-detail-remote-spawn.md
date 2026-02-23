---
status: pending
priority: p3
issue_id: "116"
tags: [code-review, agent-native, feature]
dependencies: []
---

# Extend af status <spawn-id> to resolve remote spawns

## Problem Statement

`af status <spawn-id>` (detail view) doesn't search the `RemoteSpawnStore`. If a user or agent runs `af status sprites-abc`, they get "agent not found in pool or spawn registry." The overview (`af status`) shows remote spawns, but the drill-down path is broken.

Flagged by agent-native-reviewer. This is a follow-up feature, not a regression in Phase 5.

## Findings

- `internal/daemon/status.go:235` — `BuildAgentDetail` searches Pool then SpawnRegistry, not RemoteSpawnStore
- `internal/daemon/daemon.go:339` — `handleStatusAgent` doesn't pass `d.rspawns`
- `af sessions` can list and attach to remote spawns, so the detail gap is specific to `af status`

## Proposed Solutions

### Option 1: Add RemoteSpawnStore fallback to BuildAgentDetail

**Approach:** If spawn-id not found in pool or local spawns, check RemoteSpawnStore and return a detail view.

**Effort:** 1-2 hours
**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status.go` — `BuildAgentDetail`
- `internal/daemon/daemon.go` — `handleStatusAgent`

## Acceptance Criteria

- [ ] `af status <remote-spawn-id>` returns detail for remote spawns
- [ ] `af status <remote-spawn-id> --json` returns structured output
- [ ] Tests cover remote spawn detail lookup

## Work Log

### 2026-02-22 - Initial Discovery

**By:** Claude Code (code review)

**Actions:**
- Agent-native reviewer identified detail view gap
- Confirmed this is a follow-up feature, not a Phase 5 regression
