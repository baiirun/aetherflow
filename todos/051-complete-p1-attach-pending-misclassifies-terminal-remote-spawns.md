---
status: complete
priority: p1
issue_id: "051"
tags: [code-review, reliability, cli]
dependencies: []
---

# Fix attach handling for terminal remote spawn states

## Problem Statement

`af session attach <spawn-id>` currently reports `SESSION_NOT_READY` for terminal remote spawn states (`failed`, `terminated`) when `session_id` is empty.

## Findings

- `cmd/af/cmd/sessions.go:331` treats `SessionID==""` as pending regardless of state.
- This hides terminal failures and can trigger infinite retry loops in automation.

## Proposed Solutions

### Option 1: Explicit terminal-state branch

**Approach:** Only treat `requested|spawning|unknown` as pending. Return terminal error for `failed|terminated`.

**Pros:** Clear semantics; minimal change.

**Cons:** Requires error-code additions.

**Effort:** 1-2 hours

**Risk:** Low

### Option 2: State helper function

**Approach:** Add daemon helper `IsPendingRemoteSpawn(state)` and use it in CLI.

**Pros:** Centralized state logic.

**Cons:** Slightly broader refactor.

**Effort:** 2-3 hours

**Risk:** Low

## Recommended Action

Use Option 1 now; optionally follow with Option 2 cleanup.

## Technical Details

**Affected files:**
- `cmd/af/cmd/sessions.go`

## Resources

- Branch: `feat/sprites-first-remote-spawn`

## Acceptance Criteria

- [ ] `failed` and `terminated` remote spawn states never return `SESSION_NOT_READY`
- [ ] Pending path only covers `requested|spawning|unknown`
- [ ] JSON and human output both reflect terminal failure correctly

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Captured finding from multi-agent review.

**Learnings:**
- Attach state handling needs strict terminal/pending split.
