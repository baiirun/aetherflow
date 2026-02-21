---
status: complete
priority: p2
issue_id: "060"
tags: [code-review, data-integrity, provider]
dependencies: []
---

# Persist canonical provider identity for remote runtime operations

## Problem Statement

Current Sprites flow stores a synthesized sandbox identifier and may not preserve provider-canonical identity strongly enough for robust status/terminate/reconcile.

## Findings

- `internal/daemon/sprites_client.go` maps `SandboxID` to sanitized local name and stores provider UUID in `OperationID`.
- Future status/terminate correctness depends on stable canonical provider identity across retries and normalization.

## Proposed Solutions

### Option 1: Explicit canonical fields

**Approach:** Persist both provider canonical name and provider UUID/id in dedicated fields and use canonical field for all provider follow-up calls.

**Pros:** Clear semantics and safer reconcile.

**Cons:** Schema expansion.

**Effort:** 3-5 hours

**Risk:** Medium

## Recommended Action

Implement Option 1 before expanding reconcile/terminate behavior.

## Technical Details

**Affected files:**
- `internal/daemon/sprites_client.go`
- `internal/daemon/remote_spawn_store.go`

## Acceptance Criteria

- [ ] canonical provider identity is persisted unambiguously
- [ ] status/terminate paths use canonical identity consistently
- [ ] migration/backward-read behavior is tested

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Added follow-up integrity item from data-integrity review.
