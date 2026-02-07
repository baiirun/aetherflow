---
status: pending
priority: p3
issue_id: "008"
tags: [code-review, simplicity, cleanup]
dependencies: []
---

# Eliminate QueuedTask type — add JSON tags to Task and reuse it

## Problem Statement

`QueuedTask` has the same fields as `Task` (ID, Priority, Title). It exists only to add JSON tags. `fetchQueue` manually converts `[]Task` to `[]QueuedTask` in a loop. This is unnecessary duplication.

## Findings

- `Task` (poll.go): `{ID string; Priority int; Title string}` — no JSON tags
- `QueuedTask` (status.go): `{ID string; Priority int; Title string}` — has JSON tags
- `fetchQueue` converts between them in a 5-line loop (status.go:158-165)
- Duplicate also exists in `client.QueuedTask`
- Identified by: simplicity-reviewer, pattern-recognition-specialist

## Proposed Solutions

### Option 1: Add JSON tags to Task, delete QueuedTask

**Approach:** Add `json:"..."` tags to `Task` in `poll.go`, use `[]Task` in `FullStatus.Queue`, delete `QueuedTask` and the conversion loop.

**Effort:** 20 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `Task` has JSON tags
- [ ] `QueuedTask` type removed from daemon and client
- [ ] `fetchQueue` returns `[]Task` directly
- [ ] Tests pass

## Work Log

### 2026-02-07 - Simplicity Review

**By:** Claude Code
