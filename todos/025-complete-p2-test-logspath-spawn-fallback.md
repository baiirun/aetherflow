---
status: complete
priority: p2
issue_id: "025"
tags: [code-review, testing]
dependencies: []
---

# Add test for handleLogsPath spawn registry fallback

## Problem Statement

`handleLogsPath` was restructured to fall back to the spawn registry when an agent isn't found in the pool. The existing tests cover pool lookup and not-found cases, but the new spawn fallback path is untested.

## Findings

- Found by: code-reviewer
- Location: `internal/daemon/logs_test.go` — missing `TestHandleLogsPathSpawnFallback`
- The fallback logic at `logs.go:53-62` is new and has no direct test coverage

## Proposed Solutions

### Option 1: Add TestHandleLogsPathSpawnFallback (Recommended)

**Approach:** Create a daemon with a spawn registry containing an entry, no pool agents, and verify `logs.path` returns the spawn's log path.

- **Effort:** Small
- **Risk:** Low

## Technical Details

- **Affected files:** `internal/daemon/logs_test.go`

## Acceptance Criteria

- [ ] Test that logs.path returns spawn's LogPath when agent not in pool
- [ ] Test that logs.path returns pool's log path when agent IS in pool (existing)
- [ ] Test that logs.path returns error when agent in neither (existing)

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | New code path deserves coverage |
