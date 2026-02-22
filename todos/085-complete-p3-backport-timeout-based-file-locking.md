---
status: pending
priority: p3
issue_id: "085"
tags: [code-review, consistency]
dependencies: []
---

# Backport timeout-based file locking to sessions/store.go

## Problem Statement

`internal/sessions/store.go` uses blocking `LOCK_EX` (no timeout) while the new `remote_spawn_store.go` uses `LOCK_NB` with retry loop and 5s timeout. Two stores in the same codebase with different locking strategies.

## Findings

- `internal/sessions/store.go` acquires file locks with blocking `LOCK_EX`, which can hang indefinitely if a lock is never released (e.g., process crash without cleanup).
- `internal/daemon/remote_spawn_store.go` uses the safer pattern: `LOCK_NB` with a retry loop and 5-second timeout.
- Having two different locking strategies in the same codebase for the same kind of operation (file-based store locking) is inconsistent and makes the older code a latent reliability risk.

## Proposed Solution

Backport the timeout-based locking pattern from `remote_spawn_store.go` to `sessions/store.go`.

## Acceptance Criteria

- [ ] `internal/sessions/store.go` uses `LOCK_NB` with a retry loop and timeout instead of blocking `LOCK_EX`.
- [ ] Timeout duration is consistent with `remote_spawn_store.go` (5s) or configurable.
- [ ] Existing session store tests pass.
- [ ] No deadlock risk from indefinite blocking.

## Work Log

- **Effort estimate:** Small (30 min)
