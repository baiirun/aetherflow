---
status: pending
priority: p1
issue_id: "069"
tags: [code-review, correctness, data-integrity]
dependencies: []
---

# claimSession never updates RemoteSpawnStore — session_id never bound

## Problem Statement

`internal/daemon/event_rpc.go:174-264` — `claimSession` binds sessions to the in-memory `SpawnRegistry` but never writes to `RemoteSpawnStore`. Remote spawn records never transition to `running` and never get `session_id` populated. This means `af session attach <spawn_id>` will perpetually return `SESSION_NOT_READY` for remote spawns even after the session is live.

## Findings

- Data-integrity-guardian identified this as the critical referential integrity gap.
- `claimSession` correctly updates the in-memory `SpawnRegistry`, but the persistent `RemoteSpawnStore` is never informed of the state change.
- Remote spawn records remain in their pre-claim state indefinitely, with `session_id` never populated.
- Any consumer relying on `RemoteSpawnStore` to determine session readiness (e.g., `af session attach`) will never see the session as available.

## Proposed Solution

1. Add a `RemoteSpawnStore` field to the `Daemon` struct (if not already present).
2. When `claimSession` finds a matching remote spawn candidate, also call `store.Upsert()` to update the record with `state=running` and the bound `session_id`.
3. Ensure the `Upsert` call happens atomically with the in-memory `SpawnRegistry` update to avoid inconsistent state.

**Effort:** Medium (1-2 hours)

## Acceptance Criteria

- [ ] After a plugin event arrives and `claimSession` succeeds, the corresponding `RemoteSpawnStore` record transitions to `running` with `session_id` populated.
- [ ] `af session attach <spawn_id>` succeeds for remote spawns after the session is claimed.
- [ ] Test covers the end-to-end flow: remote spawn created → plugin event → claim → store record updated.

## Work Log
