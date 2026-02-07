---
status: pending
priority: p2
issue_id: "002"
tags: [code-review, observability, logging]
dependencies: []
---

# Add daemon-side logging to BuildFullStatus and handleStatusFull

## Problem Statement

`BuildFullStatus` and `handleStatusFull` produce zero log output on the daemon side. Errors from `prog show` and `prog ready` are collected into the response `Errors` slice and sent to the client, but if the client disconnects before reading the response, those errors are lost. There's no daemon-side evidence that status requests are happening, how long they take, or whether `prog` calls are failing.

This violates the project's own logging philosophy: "Emit canonical start and end logs per request/job" with "outcome + full context (status, duration, error, key metrics)."

## Findings

- `handleStatusFull` has no log statement at all
- `BuildFullStatus` collects errors into `Errors` slice but doesn't log them
- `cfg.Logger` is available in Config but not used by `BuildFullStatus`
- Identified by: grug-brain-reviewer, architecture-strategist

**Affected files:**
- `internal/daemon/daemon.go:165-172` — handleStatusFull
- `internal/daemon/status.go:54-120` — BuildFullStatus

## Proposed Solutions

### Option 1: Canonical log line in handleStatusFull (Recommended)

**Approach:** Add a single structured log line in the daemon handler:

```go
func (d *Daemon) handleStatusFull(ctx context.Context) *Response {
    start := time.Now()
    status := BuildFullStatus(ctx, d.pool, d.config, d.config.Runner)
    d.log.Info("status.full",
        "agents", len(status.Agents),
        "queue", len(status.Queue),
        "errors", len(status.Errors),
        "duration", time.Since(start),
    )
    // ...
}
```

**Pros:**
- Single log line per request, queryable
- Duration tells you if prog is slow
- Error count tells you about partial failures
- No signature changes needed

**Cons:**
- Doesn't log individual error details (they go in the response)

**Effort:** 15 minutes
**Risk:** Low

### Option 2: Log in both handler and BuildFullStatus

**Approach:** Additionally add warn-level logs inside BuildFullStatus for individual `prog show` failures, using `cfg.Logger`.

**Pros:** Full observability, individual failure visibility
**Cons:** More verbose logs

**Effort:** 30 minutes
**Risk:** Low

## Recommended Action

Start with Option 1 — a single canonical log line in `handleStatusFull`. Add Option 2 if debugging `prog` failures proves difficult.

## Acceptance Criteria

- [ ] `handleStatusFull` logs a structured line with agent count, queue count, error count, and duration
- [ ] Existing tests still pass

## Work Log

### 2026-02-07 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified logging gap during code review
- Confirmed by grug-brain-reviewer and architecture-strategist
