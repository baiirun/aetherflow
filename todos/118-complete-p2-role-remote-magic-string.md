---
status: pending
priority: p2
issue_id: "118"
tags: [code-review, quality, pattern-consistency]
dependencies: []
---

# Add RoleRemote constant — eliminate "remote" magic string

## Problem Statement

`buildRemoteSpawnDetail` uses the bare string literal `"remote"` for the agent's role, breaking the pattern established by `RoleWorker`, `RolePlanner`, and `RoleSpawn` which are typed constants in `role.go`. The test also hardcodes `"remote"`. A typo (e.g., `"remtoe"`) would compile and pass tests silently.

## Findings

- `internal/daemon/status.go:355` — `Role: "remote"` is a bare string
- `internal/daemon/status_agent_test.go:392` — test asserts against `"remote"` literal
- `internal/daemon/status.go:321` — `buildSpawnDetail` correctly uses `string(RoleSpawn)` — this is the pattern to follow
- Flagged by: code-reviewer, grug-brain-reviewer, pattern-recognition-specialist, tigerstyle-reviewer (4/10 agents)

## Proposed Solutions

### Option 1: Add RoleRemote to role.go

**Approach:** Add `RoleRemote Role = "remote"` to the existing constants in `role.go`, use `string(RoleRemote)` in `buildRemoteSpawnDetail` and the test.

**Pros:**
- Follows the exact pattern of `RoleSpawn`
- Compile-time safety against typos
- Grep for role definitions finds all roles

**Cons:**
- None

**Effort:** 10 minutes

**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/role.go` — add `RoleRemote Role = "remote"`
- `internal/daemon/status.go:355` — change `"remote"` to `string(RoleRemote)`
- `internal/daemon/status_agent_test.go:392-393` — change `"remote"` to `string(RoleRemote)`

## Acceptance Criteria

- [ ] `RoleRemote` constant defined in `role.go`
- [ ] `buildRemoteSpawnDetail` uses `string(RoleRemote)`
- [ ] Test assertions use `string(RoleRemote)` not magic string
- [ ] `go build ./...` passes
- [ ] `go test ./internal/daemon/... -count=1` passes

## Work Log

### 2026-02-22 - Code Review Round 5

**By:** Claude Code

**Actions:**
- Identified pattern break during 10-agent parallel review
- 4 of 10 agents independently flagged this finding

**Learnings:**
- All role values should go through the typed `Role` constants
