---
status: complete
priority: p1
issue_id: "053"
tags: [code-review, reliability, idempotency]
dependencies: []
---

# Propagate request idempotency to Sprites create boundary

## Problem Statement

Local idempotency exists in the remote spawn store, but provider-side create currently ignores `request_id`, risking duplicate remote runtimes on timeout/retry ambiguity.

## Findings

- `cmd/af/cmd/spawn.go:166` passes `RequestID` to provider request.
- `internal/daemon/sprites_client.go:50` does not include idempotency metadata in provider request.
- Network timeout after provider success can produce local `failed` state and duplicate create attempts.

## Proposed Solutions

### Option 1: Provider request metadata/header idempotency

**Approach:** Send `request_id`/`spawn_id` as provider metadata (or idempotency header when supported).

**Pros:** Strong duplicate prevention.

**Cons:** Depends on provider API support.

**Effort:** 3-5 hours

**Risk:** Medium

### Option 2: Ambiguous error -> unknown + reconcile

**Approach:** On create transport errors, write `unknown` and reconcile by metadata before retry.

**Pros:** Works even without provider idempotency primitives.

**Cons:** Adds reconcile complexity.

**Effort:** 4-6 hours

**Risk:** Medium

## Recommended Action

Implement Option 1 where possible and Option 2 fallback.

## Technical Details

**Affected files:**
- `internal/daemon/sprites_client.go`
- `cmd/af/cmd/spawn.go`
- `internal/daemon/remote_spawn_store.go`

## Acceptance Criteria

- [ ] Retries with same request id do not create duplicate runtime remotely
- [ ] Create transport ambiguity does not immediately mark terminal failure
- [ ] Integration test covers timeout-after-create scenario

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Captured idempotency boundary gap from multiple reviewers.
