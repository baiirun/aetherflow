---
status: pending
priority: p2
issue_id: "113"
tags: [code-review, logging, observability]
dependencies: []
---

# Add remote_spawns count to status.full canonical log

## Problem Statement

The `status.full` canonical end-of-request log in `internal/daemon/daemon.go:366-371` emits `agents`, `queue`, `errors`, and `duration` counts but omits `remote_spawns` (and `spawns`). Per the codebase's logging philosophy, canonical logs should include all key metrics for the response. Without this, you can't query for "how many status requests returned remote spawns" or debug why remote spawns aren't showing up.

Flagged by code-reviewer and grug-brain-reviewer.

## Findings

- `internal/daemon/daemon.go:366-371` — `d.log.Info("status.full", ...)` missing remote_spawns and spawns counts
- `internal/daemon/daemon.go:344-349` — `handleStatusAgent` logs partial errors; `handleStatusFull` does not

## Proposed Solutions

### Option 1: Add spawns and remote_spawns to the canonical log

**Approach:** Add two fields to the existing log line.

```go
d.log.Info("status.full",
    "agents", len(status.Agents),
    "spawns", len(status.Spawns),
    "remote_spawns", len(status.RemoteSpawns),
    "queue", len(status.Queue),
    "errors", len(status.Errors),
    "duration", time.Since(start),
)
```

**Pros:** Complete canonical log, easy grep/query
**Cons:** Slightly wider log line
**Effort:** 5 minutes
**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/daemon.go:366-371`

## Acceptance Criteria

- [ ] `status.full` log includes `remote_spawns` and `spawns` counts
- [ ] Tests pass

## Work Log

### 2026-02-22 - Initial Discovery

**By:** Claude Code (code review)

**Actions:**
- Identified missing metric in canonical log
- Verified `handleStatusAgent` has partial error logging but `handleStatusFull` does not
