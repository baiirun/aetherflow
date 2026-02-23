---
status: pending
priority: p2
issue_id: "100"
tags: [code-review, testing, state-machine]
dependencies: []
---

# Add test for Running → Unknown state transition

## Problem Statement

The `RemoteSpawnRunning → RemoteSpawnUnknown` transition was added to the state machine (#097) but has no corresponding test case in the existing transition validation tests.

## Findings

- `internal/daemon/remote_spawn_store.go:377` — transition added
- `internal/daemon/remote_spawn_store_test.go` — existing test covers `unknown → running` but not the reverse
- The transition models real-world network partitions where a running spawn loses contact

## Proposed Solutions

### Option 1: Add test case to existing transition test (Recommended)

**Approach:** Add `{"running → unknown", RemoteSpawnRunning, RemoteSpawnUnknown, false}` to the transition validation test table.

**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Test case for `RemoteSpawnRunning → RemoteSpawnUnknown` exists
- [ ] `go test ./internal/daemon/...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified by code-reviewer agent
- The transition was added in #097 but the test was not added
