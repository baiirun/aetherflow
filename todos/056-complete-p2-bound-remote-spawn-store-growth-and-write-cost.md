---
status: complete
priority: p2
issue_id: "056"
tags: [code-review, performance, storage]
dependencies: []
---

# Add retention bounds for remote spawn store growth

## Problem Statement

`remote_spawns.json` currently grows unbounded and each upsert rewrites the full file under lock, creating long-term latency and contention risk.

## Findings

- `internal/daemon/remote_spawn_store.go` performs full read/scan/write for `Upsert`.
- No retention pruning or max-record controls are implemented.

## Proposed Solutions

### Option 1: Add TTL/max-record sweep

**Approach:** Add `SweepStale` and optional max-record pruning for terminal records.

**Pros:** Minimal change; immediate protection.

**Cons:** Still O(n) writes.

**Effort:** 2-4 hours

**Risk:** Low

### Option 2: Move to SQLite/Bbolt

**Approach:** Replace JSON file with indexed store and unique constraints.

**Pros:** Scales better; faster lookups/updates.

**Cons:** Larger migration effort.

**Effort:** 1-2 days

**Risk:** Medium

## Recommended Action

Ship Option 1 now; evaluate Option 2 if remote spawn volume grows.

## Technical Details

**Affected files:**
- `internal/daemon/remote_spawn_store.go`
- `internal/daemon/daemon.go` (sweep wiring)

## Acceptance Criteria

- [ ] remote spawn records are pruned by TTL and/or cap
- [ ] lookup/update behavior remains deterministic
- [ ] tests cover sweep behavior

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Captured perf/scalability concern from performance and tigerstyle reviewers.
