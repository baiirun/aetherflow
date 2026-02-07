---
status: pending
priority: p2
issue_id: "014"
tags: [code-review, performance, reliability]
dependencies: []
---

# Add context/timeout to `ParseToolCalls` for consistency

## Problem Statement

`fetchTaskSummary` runs with a 5s context timeout, but `ParseToolCalls` has no timeout — it reads the entire file synchronously. On a slow filesystem or with a very large log file (long-running agent), this blocks the RPC response indefinitely. Asymmetric timeout handling between the two concurrent operations in `BuildAgentDetail`.

## Findings

- Found by: architecture-strategist (primary), performance-oracle, security-sentinel
- `fetchTaskSummary` goroutine: `context.WithTimeout(ctx, 5*time.Second)` ✅
- `ParseToolCalls` goroutine: no timeout, reads entire file ❌
- For a 24-hour agent with 60MB+ log, parse could take 1-3 seconds
- On NFS or slow disk, could block indefinitely

## Proposed Solutions

### Option 1: Thread context through ParseToolCalls

**Approach:** Add `ctx context.Context` parameter, check `ctx.Err()` in scan loop. Wrap with 5s timeout in `BuildAgentDetail`.

**Pros:** Consistent with existing pattern, cancellable
**Cons:** Adds context parameter to pure parser function
**Effort:** 15 minutes
**Risk:** Low

### Option 2: Tail-seek optimization (solves performance too)

**Approach:** For limited queries (limit > 0), seek to near the end of the file and scan forward. Reads ~300KB instead of the full file.

**Pros:** Solves both timeout and performance concerns, O(1) instead of O(file_size)
**Cons:** More complex implementation
**Effort:** 1 hour
**Risk:** Medium

## Recommended Action

Option 1 now (quick, consistent), Option 2 when `--watch` drives the need.

## Acceptance Criteria

- [ ] `ParseToolCalls` accepts `context.Context`
- [ ] Checks `ctx.Err()` periodically in scan loop
- [ ] `BuildAgentDetail` wraps parse call with 5s timeout
- [ ] Tests updated for new signature

## Work Log

### 2026-02-07 - Discovery

**By:** Multi-agent code review (3 agents converged on this)
