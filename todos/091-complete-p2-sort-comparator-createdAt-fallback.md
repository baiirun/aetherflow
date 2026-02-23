---
status: pending
priority: p2
issue_id: "091"
tags: [code-review, correctness]
dependencies: []
---

# Sort comparator should use `CreatedAt` fallback when `UpdatedAt` is zero

## Problem Statement

The sort in `buildSessionListEntries` uses `UpdatedAt` for ordering, but the display code falls back to `CreatedAt` when `UpdatedAt` is zero. This means the visual "UPDATED" column and the actual sort order can disagree — entries sorted by zero `UpdatedAt` (pushed to bottom) might display a recent `CreatedAt`, confusing users.

## Findings

- `cmd/af/cmd/sessions.go:232-234` — sort by `UpdatedAt` only
- `cmd/af/cmd/sessions.go:154-156` — display falls back to `CreatedAt` when `UpdatedAt` is zero
- `RemoteSpawnRecord.UpdatedAt` is set on upsert but could be zero from schema migration or corruption

## Proposed Solutions

### Option 1: Use same fallback in sort comparator

**Approach:**
```go
sort.Slice(entries, func(i, j int) bool {
    ti := entries[i].UpdatedAt
    if ti.IsZero() { ti = entries[i].CreatedAt }
    tj := entries[j].UpdatedAt
    if tj.IsZero() { tj = entries[j].CreatedAt }
    return ti.After(tj)
})
```

**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Sort uses `CreatedAt` fallback when `UpdatedAt` is zero
- [ ] Display order matches visual "UPDATED" column

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified mismatch between sort and display logic for timestamps
