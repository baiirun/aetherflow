---
status: complete
priority: p3
issue_id: "058"
tags: [code-review, cli, quality]
dependencies: []
---

# Validate spawn provider flag values explicitly

## Problem Statement

Unknown `--provider` values currently fall back to local spawn behavior instead of failing fast.

## Findings

- `cmd/af/cmd/spawn.go:103` only special-cases `sprites`; any typo silently uses local path.

## Proposed Solutions

### Option 1: strict enum validation

**Approach:** Reject provider not in `{local,sprites}` with clear error.

**Pros:** Prevents accidental mode mismatch.

**Cons:** None significant.

**Effort:** <1 hour

**Risk:** Low

## Recommended Action

Implement Option 1.

## Technical Details

**Affected files:**
- `cmd/af/cmd/spawn.go`

## Acceptance Criteria

- [ ] invalid provider value returns explicit error
- [ ] valid values preserve existing behavior
- [ ] tests cover invalid provider

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Captured quality footgun from multiple reviewers.
