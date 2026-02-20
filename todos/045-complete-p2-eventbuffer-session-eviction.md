---
status: complete
priority: p2
issue_id: "045"
tags: [code-review, memory, reliability]
dependencies: []
---

# Add session eviction to EventBuffer

## Problem Statement

`EventBuffer` bounds events per session at 2000 but the number of sessions in the map is unbounded. `Clear()` exists but is never called in production code. Over a long-running daemon lifetime with many task cycles, dead sessions accumulate indefinitely — each holding up to 4-20MB of event data.

## Findings

- `EventBuffer.sessions` map grows without bound (confirmed: `Clear` has 0 production callers)
- Per-session memory: ~4MB conservative, ~20MB worst case (2000 events × varying payload)
- After 50 task cycles: 200MB-1GB of unreachable event data
- The spawn registry has a similar cleanup mechanism (SweepDead with TTL) but EventBuffer does not
- Flagged by performance-oracle and tigerstyle-reviewer agents

## Proposed Solutions

### Option 1: Call Clear() on session teardown (Recommended)

**Approach:** In `reap()` or wherever session lifecycle ends, call `d.events.Clear(sessionID)`.

**Pros:** Simple, targeted, matches existing cleanup patterns
**Cons:** Events disappear immediately after agent exit (no post-mortem window)
**Effort:** 15 minutes
**Risk:** Low

### Option 2: TTL-based session eviction

**Approach:** Add a periodic sweep that clears sessions older than 1 hour (matching spawn registry TTL).

**Pros:** Provides post-mortem window, consistent with spawn sweep pattern
**Cons:** More code, another goroutine
**Effort:** 30 minutes
**Risk:** Low

### Option 3: Max-sessions limit with LRU eviction

**Approach:** Cap total sessions (e.g., 50), evict oldest when at capacity.

**Pros:** Hard memory bound, predictable
**Cons:** Most complex, may evict active sessions under heavy use
**Effort:** 45 minutes
**Risk:** Medium

## Recommended Action



## Acceptance Criteria

- [ ] EventBuffer does not grow unbounded over daemon lifetime
- [ ] Dead sessions are eventually cleaned up
- [ ] Active sessions are not affected
- [ ] Test verifies cleanup behavior

## Work Log

### 2026-02-19 - Code Review Discovery

**By:** Claude Code (performance-oracle + tigerstyle-reviewer)

**Actions:**
- Identified unbounded session map via code analysis
- Confirmed Clear() is test-only via grep
- Estimated memory impact per session
