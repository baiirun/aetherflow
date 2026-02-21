---
status: complete
priority: p1
issue_id: "052"
tags: [code-review, security, routing]
dependencies: []
---

# Enforce server filter in spawn-id attach path

## Problem Statement

`af session attach <spawn-id> --server <ref>` can still attach to a different server because the spawn fallback path does not enforce `--server`.

## Findings

- `cmd/af/cmd/sessions.go:330-337` loads remote spawn by spawn ID and appends a match without validating `serverFilter`.
- This violates explicit user intent and can route to the wrong target.

## Proposed Solutions

### Option 1: Hard check before append

**Approach:** If `serverFilter` is set and `rs.ServerRef != serverFilter`, treat as not found.

**Pros:** Simple, explicit.

**Cons:** None significant.

**Effort:** <1 hour

**Risk:** Low

### Option 2: Central resolver function

**Approach:** Create one resolver that applies filtering for both session-id and spawn-id lookup.

**Pros:** Prevents future drift.

**Cons:** Slightly more refactor.

**Effort:** 2-3 hours

**Risk:** Low

## Recommended Action

Ship Option 1 immediately.

## Technical Details

**Affected files:**
- `cmd/af/cmd/sessions.go`

## Acceptance Criteria

- [ ] `--server` is honored for spawn-id lookup path
- [ ] No attach occurs if server filter mismatches
- [ ] Tests cover mismatch and match cases

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Recorded server filter bypass defect from review agents.
