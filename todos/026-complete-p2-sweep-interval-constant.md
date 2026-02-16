---
status: complete
priority: p2
issue_id: "026"
tags: [code-review, quality]
dependencies: []
---

# Use named constant for spawn sweep interval

## Problem Statement

The spawn sweep interval is hardcoded as `30 * time.Second` in `sweepSpawns`. The pool's sweep uses `sweepInterval` (a package-level const). The spawn sweep should reference the same constant or define its own.

## Findings

- Found by: code-reviewer, pattern-recognition-specialist
- Location: `internal/daemon/daemon.go:175`
- Pool uses `const sweepInterval = 30 * time.Second` (pool.go)

## Proposed Solutions

### Option 1: Reuse pool's sweepInterval (Recommended)

Both are in the `daemon` package, so just reference `sweepInterval`.

- **Effort:** Tiny
- **Risk:** None

## Technical Details

- **Affected files:** `internal/daemon/daemon.go`

## Acceptance Criteria

- [ ] Spawn sweep uses a named constant, not a magic number

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | |
