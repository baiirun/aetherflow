---
status: pending
priority: p2
issue_id: "003"
tags: [code-review, correctness, timeout]
dependencies: []
---

# fetchQueue has no timeout unlike per-agent enrichment calls

## Problem Statement

In `BuildFullStatus`, each `prog show` call gets a 5-second timeout via `context.WithTimeout(ctx, 5*time.Second)`, but `fetchQueue` uses the raw parent context with no explicit timeout. If `prog ready` hangs, the entire status call blocks indefinitely (or until the client disconnects). This asymmetry is inconsistent and could cause the daemon's RPC handler goroutine to leak.

## Findings

- `fetchTaskSummary` goroutines: 5s timeout (status.go:85)
- `fetchQueue`: no timeout (status.go:113)
- `fetchQueue` runs after `wg.Wait()`, so it's sequential and blocks the response
- Identified by: code-reviewer, architecture-strategist, performance-oracle

**Affected file:** `internal/daemon/status.go:113`

## Proposed Solutions

### Option 1: Add context.WithTimeout to fetchQueue (Recommended)

**Approach:**

```go
queueCtx, queueCancel := context.WithTimeout(ctx, 5*time.Second)
defer queueCancel()
queue, err := fetchQueue(queueCtx, cfg.Project, runner)
```

**Pros:**
- Symmetric with agent enrichment timeouts
- Prevents unbounded blocking
- Simple, one-line change

**Cons:** None

**Effort:** 5 minutes
**Risk:** Low

### Option 2: Add overall timeout to BuildFullStatus

**Approach:** Wrap the entire function in a 10-second timeout.

**Pros:** Caps total wall time
**Cons:** Less granular; if agents take 9s, queue gets only 1s

**Effort:** 10 minutes
**Risk:** Low

## Recommended Action

Option 1 — add a 5-second timeout to the fetchQueue call for symmetry.

## Acceptance Criteria

- [ ] `fetchQueue` call uses `context.WithTimeout(ctx, 5*time.Second)`
- [ ] Existing tests still pass

## Work Log

### 2026-02-07 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified timeout asymmetry during code review
- Confirmed by 3 independent review agents
