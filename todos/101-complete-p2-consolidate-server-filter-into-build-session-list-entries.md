---
status: pending
priority: p2
issue_id: "101"
tags: [code-review, simplicity, locality-of-behavior]
dependencies: []
---

# Consolidate server filtering into buildSessionListEntries

## Problem Statement

Server filtering is split between two places: `runSessions` filters session records (lines 116-124), then `buildSessionListEntries` filters remote spawn records (line 209). This split means:
1. The `serverFilter` parameter name in `buildSessionListEntries` is misleading — it only applies to remote records
2. The session-record filter path is untested because it happens outside the tested function
3. Locality of behavior is violated — filtering logic is scattered

## Findings

- `cmd/af/cmd/sessions.go:116-124` — session record filtering in caller
- `cmd/af/cmd/sessions.go:209` — remote spawn filtering in callee
- `buildSessionListEntries` already takes `serverFilter` — it should apply it uniformly

## Proposed Solutions

### Option 1: Move all filtering into buildSessionListEntries (Recommended)

**Approach:** Remove the 8-line filter block from `runSessions` and add session-record filtering inside `buildSessionListEntries`.

**Pros:** Single owner of "what gets included"; fully testable; -8 LOC
**Cons:** None
**Effort:** 10 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `runSessions` no longer filters `recs` before calling `buildSessionListEntries`
- [ ] `buildSessionListEntries` applies `serverFilter` to both session records and remote spawns
- [ ] Add test case for session-record filtering in `TestBuildSessionListEntries`
- [ ] `go test ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified by simplicity-reviewer agent
