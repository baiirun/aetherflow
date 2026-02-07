---
status: pending
priority: p3
issue_id: "015"
tags: [code-review, observability, logging]
dependencies: []
---

# Log actual error details in `handleStatusAgent`, not just count

## Problem Statement

The `handleStatusAgent` RPC handler logs `"errors", len(detail.Errors)` — the operator knows something went wrong but not what. If prog times out AND jsonl parse fails, the log shows `errors=2` but not which two errors occurred.

## Findings

- Found by: grug-brain
- daemon.go handler logs: `d.log.Info("status.agent", ..., "errors", len(detail.Errors), ...)`
- Error details only visible to the CLI client, not in daemon logs
- Similar pattern exists in `handleStatusFull` — same issue there

## Proposed Solutions

### Option 1: Log each partial error at Warn level

**Approach:** After the canonical Info log, iterate errors and log each:
```go
for _, e := range detail.Errors {
    d.log.Warn("status.agent.partial_error", "agent", params.AgentName, "error", e)
}
```

**Effort:** 5 minutes
**Risk:** None

## Acceptance Criteria

- [ ] Each partial error logged individually at Warn level
- [ ] Existing canonical Info log unchanged

## Work Log

### 2026-02-07 - Discovery

**By:** Multi-agent code review (grug-brain)
