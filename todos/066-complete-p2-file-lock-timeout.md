---
status: complete
priority: p2
issue_id: "066"
tags: [code-review, reliability, locking]
dependencies: []
---

# Add timeout to file lock (LOCK_EX is currently unbounded)

## Problem Statement

`RemoteSpawnStore.lockFile()` uses `syscall.Flock(fd, LOCK_EX)` which blocks indefinitely if another process holds the lock. A stuck or crashed process can cause all CLI invocations to hang forever.

## Recommended Action

Use `LOCK_EX|LOCK_NB` (non-blocking) in a retry loop with a bounded timeout (e.g., 5 seconds with exponential backoff). If the timeout expires, return a clear error.

## Technical Details

**Affected files:**
- `internal/daemon/remote_spawn_store.go` — `lockFile()`

## Acceptance Criteria

- [ ] File lock has a bounded timeout
- [ ] Timeout error message is clear and actionable
- [ ] Test covers timeout behavior (or at minimum, non-blocking acquisition)
