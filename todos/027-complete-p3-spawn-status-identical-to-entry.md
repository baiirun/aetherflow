---
status: complete
priority: p3
issue_id: "027"
tags: [code-review, simplicity]
dependencies: []
---

# Consider eliminating SpawnStatus — identical to SpawnEntry

## Problem Statement

`SpawnStatus` (status.go:25-31) and `SpawnEntry` (spawn_registry.go:12-18) have identical fields and JSON tags. The conversion in `BuildFullStatus` is a no-op copy. The same type is also duplicated in `client/client.go`.

## Findings

- Found by: code-simplicity-reviewer
- `SpawnStatus` and `SpawnEntry` are field-for-field identical
- The manual copy loop in BuildFullStatus (lines 141-149) copies identical fields

## Proposed Solutions

### Option 1: Use SpawnEntry directly in FullStatus

Remove `SpawnStatus` from `status.go`. Use `[]SpawnEntry` in `FullStatus.Spawns`. Keep the client-side `SpawnStatus` as the JSON deserialization target (it's in a different package).

- **Pros:** Removes ~15 LOC, eliminates no-op copy
- **Cons:** Exposes internal type in API response (minor — both have same JSON)
- **Effort:** Small
- **Risk:** Low

### Option 2: Keep as-is for API contract clarity

The separation documents that `SpawnEntry` is internal and `SpawnStatus` is the wire type, even if they're currently identical.

- **Pros:** Clear boundary, safe to diverge later
- **Cons:** Maintains duplication

## Recommended Action

Option 2 — keep as-is. Follows the existing AgentStatus/Agent pattern. The duplication is small and documents the boundary.

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | Follows existing pattern — likely intentional |
