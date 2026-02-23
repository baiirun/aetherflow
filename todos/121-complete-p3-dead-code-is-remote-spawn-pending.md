---
status: pending
priority: p3
issue_id: "121"
tags: [code-review, dead-code, simplification]
dependencies: []
---

# Remove unused client.IsRemoteSpawnPending helper

## Problem Statement

`client.IsRemoteSpawnPending` has zero callers in the CLI code. The CLI display handles pending as the `default` case in its switch statements, making the explicit predicate unnecessary. The `sessions.go` code uses `daemon.IsRemoteSpawnPending` (different package, different signature), not the client version.

## Findings

- `internal/client/client.go:153-156` — `IsRemoteSpawnPending` defined but unused
- `cmd/af/cmd/status.go:277` — `default` case handles pending without calling the helper
- `internal/sessions/sessions.go:480` — uses `daemon.IsRemoteSpawnPending` (takes `*RemoteSpawnRecord`)
- Flagged by: code-simplicity-reviewer

## Proposed Solutions

### Option 1: Delete IsRemoteSpawnPending from client package

**Approach:** Remove the function. Check if `RemoteSpawnUnknown` constant is still needed.

**Effort:** 5 minutes
**Risk:** Low

### Option 2: Keep for symmetry / future use

**Approach:** Keep for API completeness. If the CLI ever needs an explicit pending check, it's there.

**Effort:** 0 minutes
**Risk:** None

## Recommended Action

## Technical Details

**Affected files:**
- `internal/client/client.go:153-156`

## Acceptance Criteria

- [ ] No unused exported functions in client package
- [ ] `go build ./...` passes

## Work Log

### 2026-02-22 - Code Review Round 5

**By:** Claude Code

**Actions:**
- Identified dead code during simplicity review
