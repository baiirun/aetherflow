---
status: complete
priority: p1
issue_id: "017"
tags: [code-review, security, safety]
dependencies: []
---

# Cap SpawnRegistry size to prevent memory exhaustion

## Problem Statement

The `SpawnRegistry` has no upper bound on entries. Any process with access to the Unix socket can spam `spawn.register` RPCs. Combined with PID spoofing (registering PID 1 creates immortal entries that survive sweep), this enables memory exhaustion of the daemon. The `Prompt` field is particularly dangerous — unbounded string stored in memory per entry.

The Pool has `PoolSize` as its bound. The SpawnRegistry has nothing.

## Findings

- Found by: security-sentinel, tigerstyle-reviewer, performance-oracle, data-integrity-guardian
- Location: `internal/daemon/spawn_registry.go:38-42` (Register), `internal/daemon/spawn_rpc.go:32-38` (handler)
- The pool is bounded by `PoolSize` (typically 3-5). The spawn registry is unbounded.
- Birthday paradox: ~120 concurrent spawns gives ~50% collision probability on IDs too (related: 019)

## Proposed Solutions

### Option 1: Add maxEntries constant and reject when full (Recommended)

**Approach:** Add a `maxSpawnEntries` constant (e.g., 128). Reject registration when full.

```go
const maxSpawnEntries = 128

func (r *SpawnRegistry) Register(entry SpawnEntry) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if _, exists := r.entries[entry.SpawnID]; !exists && len(r.entries) >= maxSpawnEntries {
        return fmt.Errorf("spawn registry full (%d entries)", maxSpawnEntries)
    }
    r.entries[entry.SpawnID] = &entry
    return nil
}
```

The RPC handler must propagate the error.

- **Pros:** Simple, deterministic bound, prevents OOM
- **Cons:** Existing callers ignore Register return (need to update)
- **Effort:** Small
- **Risk:** Low — just a cap check

### Option 2: Cap field lengths instead of entry count

**Approach:** Truncate `Prompt` to 8KB and `LogPath` to 4KB at registration time. Keep entry count unbounded.

- **Pros:** Bounds memory per entry
- **Cons:** Doesn't prevent entry count explosion; truncation loses data
- **Effort:** Small
- **Risk:** Low

### Option 3: Both — cap entries AND field lengths

- **Pros:** Defense in depth
- **Effort:** Small
- **Risk:** Low

## Recommended Action

Option 3 — cap entries at 128 AND cap Prompt at 8KB, LogPath at 4KB, SpawnID at 128 chars. This matches the TigerStyle "explicit limits" principle.

## Technical Details

- **Affected files:** `spawn_registry.go`, `spawn_rpc.go`
- **Components:** SpawnRegistry, handleSpawnRegister
- **Database changes:** None (in-memory only)

## Acceptance Criteria

- [ ] `Register` returns error when registry is full
- [ ] RPC handler rejects oversized Prompt, LogPath, SpawnID
- [ ] Test for registry-full rejection
- [ ] Test for field length validation

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | Found by 4 reviewers independently — strong signal |

## Resources

- Related: #019 (SpawnID collision)
- TigerStyle rules: explicit limits, no unbounded data structures
