---
status: complete
priority: p2
issue_id: "064"
tags: [code-review, data-safety, store]
dependencies: []
---

# Store pruning must never drop non-terminal records

## Problem Statement

`pruneRemoteSpawnRecords()` applies TTL-based eviction only to terminal states (correct), but when the record count exceeds `remoteSpawnMaxRecords` (512), it sorts by `UpdatedAt` and hard-truncates. This truncation can drop non-terminal (running/spawning) records, losing track of active spawns.

## Recommended Action

Partition records into terminal and non-terminal before pruning. Only evict terminal records to get under the cap. If non-terminal records alone exceed the cap, log a warning but never drop them.

## Technical Details

**Affected files:**
- `internal/daemon/remote_spawn_store.go` — `pruneRemoteSpawnRecords()`

## Acceptance Criteria

- [ ] Non-terminal records are never dropped by pruning
- [ ] Terminal records are evicted first (oldest first) to respect the cap
- [ ] Test covers scenario where non-terminal count exceeds cap
