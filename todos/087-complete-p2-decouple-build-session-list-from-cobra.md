---
status: pending
priority: p2
issue_id: "087"
tags: [code-review, architecture, testability]
dependencies: []
---

# Decouple `buildSessionListEntries` from `*cobra.Command`

## Problem Statement

`buildSessionListEntries(cmd *cobra.Command, ...)` takes a cobra command solely to pass it to `openRemoteSpawnStore(cmd)`. This couples the core merge logic to the CLI framework, making the most important new function in Phase 4 impossible to unit test without constructing a full cobra command.

## Findings

- `cmd/af/cmd/sessions.go:182` — function signature takes `*cobra.Command`
- The only use of `cmd` is `openRemoteSpawnStore(cmd)` at line 198
- `sessions_test.go` has no test for `buildSessionListEntries` itself — only leaf functions are tested
- Reported by: code-reviewer, simplicity-reviewer, architecture-strategist, tigerstyle-reviewer

## Proposed Solutions

### Option 1: Pass remote spawn records directly

**Approach:** Change signature to accept `[]daemon.RemoteSpawnRecord` instead of `*cobra.Command`. The caller (`runSessions`) opens the remote spawn store and passes the records.

**Pros:**
- Pure function — trivially testable
- No framework dependency
- Caller controls error handling for store open

**Cons:**
- Moves store-open code to `runSessions` (slightly more code there)

**Effort:** 20 minutes
**Risk:** Low

## Technical Details

**Affected files:**
- `cmd/af/cmd/sessions.go` — change `buildSessionListEntries` signature, update `runSessions` caller

## Acceptance Criteria

- [ ] `buildSessionListEntries` does not take `*cobra.Command`
- [ ] Function is testable without cobra (see #088)
- [ ] `go build ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified cobra coupling in merge function
- Confirmed no test exists for this function due to the coupling
