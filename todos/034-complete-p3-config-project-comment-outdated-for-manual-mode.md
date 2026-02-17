---
status: complete
priority: p3
issue_id: "034"
tags: [code-review, daemon, maintainability, docs]
dependencies: []
---

# Config comment still says project is required unconditionally

## Problem Statement

The `Config.Project` field comment states “Required,” but validation now makes project conditional on `spawn_policy`. This mismatch can mislead future changes and reviews.

## Findings

- Field comment: `Project is the prog project to watch for tasks. Required.` (`internal/daemon/config.go:54`).
- Validation logic: project required only when `spawn_policy=auto` (`internal/daemon/config.go:156-159`).

## Proposed Solutions

### Option 1: Update comment to match current contract (recommended)

- Example: `Project is required in auto mode and optional in manual mode.`
- **Effort:** Small
- **Risk:** Low

## Recommended Action

Option 1.

## Technical Details

- Affected files: `internal/daemon/config.go`
- Components: inline API documentation

## Acceptance Criteria

- [x] `Config.Project` comment accurately describes conditional requirement semantics.
- [x] Comment aligns with validation and README wording.

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from workflow review | Inline docs drifted from updated policy validation behavior. |
| 2026-02-16 | Updated `Config.Project` comment to conditional requirement wording | Inline docs now align with validation behavior and user docs. |

## Resources

- Review context: local working tree on `main`
