---
status: complete
priority: p3
issue_id: "059"
tags: [code-review, architecture, maintainability]
dependencies: []
---

# Reduce CLI-level orchestration coupling in remote spawn flows

## Problem Statement

Remote spawn orchestration logic currently lives in command handlers, increasing coupling and duplication risk as provider/reconcile flows evolve.

## Findings

- `cmd/af/cmd/spawn.go` handles persistence transitions and provider calls directly.
- `cmd/af/cmd/sessions.go` duplicates config/session-dir resolution logic and interprets remote lifecycle states inline.

## Proposed Solutions

### Option 1: Extract daemon service methods

**Approach:** Move orchestration into daemon package (`StartRemoteSpawn`, `ResolveAttachTarget`) and keep cmd layer as IO shell.

**Pros:** Better boundaries and reuse.

**Cons:** Non-trivial refactor.

**Effort:** 1 day

**Risk:** Medium

### Option 2: Small helper extraction only

**Approach:** Shared session-dir resolver and state predicate helpers.

**Pros:** Low-risk incremental cleanup.

**Cons:** Doesn’t fully solve boundary concerns.

**Effort:** 2-4 hours

**Risk:** Low

## Recommended Action

Do Option 2 now, schedule Option 1 after MVP lands.

## Technical Details

**Affected files:**
- `cmd/af/cmd/spawn.go`
- `cmd/af/cmd/sessions.go`

## Acceptance Criteria

- [ ] duplicated config/session-dir resolution is removed
- [ ] lifecycle state checks use centralized helper(s)
- [ ] no behavior regression in local spawn/session flows

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Captured architecture/pattern-consistency follow-up from reviewers.
