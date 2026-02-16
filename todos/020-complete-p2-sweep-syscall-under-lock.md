---
status: complete
priority: p2
issue_id: "020"
tags: [code-review, performance, safety]
dependencies: ["017"]
---

# Move pidAlive syscalls out of write lock in SweepDead

## Problem Statement

`SweepDead` calls `r.pidAlive(entry.PID)` — a `kill(pid, 0)` syscall (~1-5µs) — while holding the write lock. This blocks all concurrent `Get`, `List`, `Register`, and `Deregister` calls for O(N) × syscall_latency. The pool has the same pattern but is bounded by `PoolSize` (3-5). The spawn registry is currently unbounded (see #017).

## Findings

- Found by: tigerstyle-reviewer, performance-oracle, security-sentinel
- Location: `internal/daemon/spawn_registry.go:75-87`
- Pool uses identical pattern (pool.go:535) but is bounded by PoolSize
- At 50 entries: ~50-250µs lock hold. At 100: ~100-500µs.

## Proposed Solutions

### Option 1: Two-phase sweep (Recommended)

**Approach:** Collect dead IDs under read lock, then delete under write lock.

```go
func (r *SpawnRegistry) SweepDead() int {
    r.mu.RLock()
    var dead []string
    for id, entry := range r.entries {
        if !r.pidAlive(entry.PID) {
            dead = append(dead, id)
        }
    }
    r.mu.RUnlock()
    if len(dead) == 0 { return 0 }
    r.mu.Lock()
    defer r.mu.Unlock()
    removed := 0
    for _, id := range dead {
        if _, exists := r.entries[id]; exists {
            delete(r.entries, id)
            removed++
        }
    }
    return removed
}
```

- **Pros:** Read lock during syscalls, write lock only for deletes
- **Cons:** Slightly more complex (TOCTOU re-check), extra allocation for dead slice
- **Effort:** Small
- **Risk:** Low — re-check handles race between phases

### Option 2: Leave as-is with size cap (if #017 caps at 128)

If the registry is capped at 128 entries, worst case is ~640µs — acceptable.

- **Pros:** No code change
- **Cons:** Still holds lock longer than necessary
- **Effort:** None
- **Risk:** None

## Recommended Action

If #017 caps the registry, Option 2 is fine — add a comment noting the tradeoff. If uncapped, Option 1.

## Technical Details

- **Affected files:** `spawn_registry.go`

## Acceptance Criteria

- [ ] Either two-phase sweep OR comment explaining bounded-size justification
- [ ] No regression in sweep tests

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | Pool has same pattern — consider fixing both |
