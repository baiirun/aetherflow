---
status: complete
priority: p2
issue_id: "037"
tags: [code-review, performance, daemon, status]
dependencies: []
---

# Bound status.full enrichment concurrency

## Problem Statement

`status.full` in auto mode can issue one prog call per agent plus queue calls per request. With watch mode and multiple clients, this can amplify load and degrade responsiveness.

## Findings

- `internal/daemon/status.go:94` starts one goroutine per agent for `prog show` enrichment.
- `internal/daemon/status.go:124` also performs queue fetch (`prog ready`).
- `af status --watch` supports short intervals, increasing call rate under scale.

## Proposed Solutions

### Option 1: Add bounded worker pool for enrichment

**Approach:** Replace per-agent unbounded fan-out with fixed worker count.

**Pros:**
- Predictable upper bound on concurrent prog calls.
- Better behavior under large agent counts.

**Cons:**
- Slightly longer latency for very large pools.

**Effort:** Medium

**Risk:** Low

---

### Option 2: Add short TTL cache for task summaries/queue

**Approach:** Cache recent status enrichments for 1-2 seconds and reuse for concurrent/polling requests.

**Pros:**
- Major reduction in repeated prog calls.

**Cons:**
- Potentially stale status for short intervals.

**Effort:** Medium

**Risk:** Medium

## Recommended Action

Defer by design for current scale profile; revisit if pool size/watcher count increases.

## Technical Details

- Affected files: `internal/daemon/status.go`, potentially `cmd/af/cmd/status.go`
- Components: status RPC path and CLI watch behavior
- Database changes: No

## Resources

- Review target: commit `006131715aed57dedad6bda8871350e5401f8816`

## Acceptance Criteria

- [x] Decision made to defer concurrency/caching optimization at current expected scale.
- [x] Existing behavior retained with no correctness regression.
- [x] Tracking retained for future scale-triggered revisit.

## Work Log

### 2026-02-16 - Initial review finding

**By:** Claude Code

**Actions:**
- Recorded scaling risk and likely hot path (`status.full`) under watch mode.

**Learnings:**
- Manual mode improved load, but auto mode still has fan-out growth.

### 2026-02-16 - Closed as deferred optimization

**By:** Claude Code

**Actions:**
- Reviewed expected operational scale and confirmed this is not a current bottleneck.
- Closed as deferred to avoid premature complexity.

**Learnings:**
- Keep hot-path simplicity until scale indicators justify bounded fan-out/caching.
