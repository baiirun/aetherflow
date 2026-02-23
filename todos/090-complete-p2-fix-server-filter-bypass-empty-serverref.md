---
status: pending
priority: p2
issue_id: "090"
tags: [code-review, security, correctness]
dependencies: []
---

# Fix server filter bypass for remote spawns with empty `ServerRef`

## Problem Statement

When `--server` filter is active, remote spawns with empty `ServerRef` bypass the filter and appear in results. This is inconsistent with session record filtering (which requires exact match) and is a data leakage concern — users filtering by server see unrelated records.

## Findings

- `cmd/af/cmd/sessions.go:213-215` — filter condition: `if serverFilter != "" && rs.ServerRef != "" && rs.ServerRef != serverFilter`
- The `rs.ServerRef != ""` guard means empty ServerRef records always pass
- Session record filter at line 118 does not have this bypass: `if r.ServerRef == serverFilter`
- Remote spawns in `requested` state legitimately have empty `ServerRef` (not yet assigned)
- Reported by: code-reviewer, security-sentinel

## Proposed Solutions

### Option 1: Remove the empty-ServerRef bypass

**Approach:** Change filter to `if serverFilter != "" && rs.ServerRef != serverFilter`. This excludes records with empty ServerRef when a filter is active.

**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Remote spawns with empty ServerRef are excluded when `--server` is active
- [ ] Remote spawns with matching ServerRef still appear
- [ ] Remote spawns with empty ServerRef still appear when no filter is active

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified inconsistent filter logic between session records and remote spawns
