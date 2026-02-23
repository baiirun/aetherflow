---
status: pending
priority: p2
issue_id: "086"
tags: [code-review, quality, correctness]
dependencies: []
---

# Add `StatusPending` and `StatusInactive` constants to sessions package

## Problem Statement

`remoteSpawnStatusToSessionStatus()` introduces two new session status values (`"pending"`, `"inactive"`) via raw string casts — `sessions.Status("pending")` — rather than using named constants. The existing statuses (`StatusActive`, `StatusIdle`, `StatusTerminated`, `StatusStale`) are all defined as typed constants. The new values bypass the type system, creating typo risk and making the status space invisible to consumers.

## Findings

- `cmd/af/cmd/sessions.go:247-253` — raw `sessions.Status("pending")`, `sessions.Status("inactive")`, `sessions.Status("unknown")` casts
- Existing pattern in `internal/sessions/store.go:23-28` — all other statuses use named constants
- The `"running"` case correctly uses `sessions.StatusActive` (line 249), making the inconsistency within the same function
- Reported by: code-reviewer, architecture-strategist, pattern-recognition, tigerstyle-reviewer (4/7 agents flagged this)

## Proposed Solutions

### Option 1: Add constants to sessions package

**Approach:** Add `StatusPending`, `StatusInactive` to `internal/sessions/store.go` alongside existing constants. Use them in `remoteSpawnStatusToSessionStatus`.

**Pros:**
- Consistent with existing pattern
- Compile-time typo protection
- Discoverable by consumers

**Cons:**
- Minor: expands the sessions package's status vocabulary

**Effort:** 15 minutes
**Risk:** Low

## Technical Details

**Affected files:**
- `internal/sessions/store.go` — add 2 constants
- `cmd/af/cmd/sessions.go` — use constants instead of raw casts

## Acceptance Criteria

- [ ] `StatusPending` and `StatusInactive` defined as constants in `internal/sessions/store.go`
- [ ] `remoteSpawnStatusToSessionStatus` uses constants, not raw string casts
- [ ] `go build ./...` passes
- [ ] `go test ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified raw string casts for new status values
- Confirmed inconsistency with existing constant pattern
- 4 of 7 review agents flagged this independently
