---
status: pending
priority: p3
issue_id: "006"
tags: [code-review, performance]
dependencies: []
---

# Parallelize fetchQueue with agent enrichment in BuildFullStatus

## Problem Statement

`fetchQueue` runs sequentially after all `prog show` goroutines complete (after `wg.Wait()`). This adds one full `prog` invocation latency (50-200ms) to every status call unnecessarily. Since the queue fetch is independent of agent enrichment, they can run concurrently.

## Findings

- `fetchQueue` called at status.go:113, after `wg.Wait()` on line 102
- Queue fetch is independent of agent data — no data dependency
- Projected savings: 50-200ms per call (one `prog` invocation removed from critical path)
- Most impactful for the planned `--watch` mode (ts-59c984) at 1-2s polling intervals
- Identified by: performance-oracle

## Proposed Solutions

### Option 1: Launch queue fetch in a separate goroutine alongside agent enrichment

**Effort:** 15 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `fetchQueue` runs concurrently with agent enrichment goroutines
- [ ] Tests still pass with `-race`

## Work Log

### 2026-02-07 - Performance Review

**By:** Claude Code
