---
status: pending
priority: p2
issue_id: "102"
tags: [code-review, testing, test-hygiene]
dependencies: []
---

# Relocate daemon predicate tests from cmd package

## Problem Statement

`TestIsRemoteSpawnPending` and `TestIsRemoteSpawnTerminal` in `cmd/af/cmd/sessions_test.go` test functions from `internal/daemon/remote_spawn_store.go`. These tests:
1. Won't run when testing only `internal/daemon/...`
2. Give false confidence about daemon package coverage
3. Are in the wrong package boundary

## Findings

- `cmd/af/cmd/sessions_test.go:136-175` — tests `daemon.IsRemoteSpawnPending` and `daemon.IsRemoteSpawnTerminal`
- These functions live in `internal/daemon/remote_spawn_store.go`
- Should be in `internal/daemon/remote_spawn_store_test.go`

## Proposed Solutions

### Option 1: Move tests to daemon package (Recommended)

**Approach:** Move `TestIsRemoteSpawnPending` and `TestIsRemoteSpawnTerminal` to `internal/daemon/remote_spawn_store_test.go`. Remove from `sessions_test.go`.

**Pros:** Tests run with the package they test; proper coverage attribution
**Cons:** Minor test relocation effort
**Effort:** 10 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Tests moved to `internal/daemon/remote_spawn_store_test.go`
- [ ] Removed from `cmd/af/cmd/sessions_test.go`
- [ ] `go test ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified by simplicity-reviewer agent
