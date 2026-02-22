---
status: pending
priority: p2
issue_id: "075"
tags: [code-review, correctness, data-integrity]
dependencies: []
---

# unknown state has no resolution path and is never pruned

## Problem Statement

`internal/daemon/remote_spawn_store.go` — The plan specifies that `unknown` auto-resolves to `failed` after 120s, but no reconciliation loop exists. `unknown` records are non-terminal so they are never pruned, accumulating indefinitely. Combined with `isRemoteSpawnPending` treating `unknown` as pending, users poll forever.

## Findings

- The `unknown` state is defined and assigned but never transitions to any terminal state.
- The design specification calls for auto-resolution to `failed` after 120 seconds, but no reconciliation loop or timer implements this.
- `unknown` is non-terminal, so the pruning logic (which only prunes terminal states) never removes these records.
- `isRemoteSpawnPending` treats `unknown` as a pending state, causing the attach flow to poll indefinitely for a state transition that will never occur.
- Over time, `unknown` records accumulate without bound, growing the store indefinitely.

## Proposed Solution

Either:

1. Implement the reconciliation timeout: a periodic loop that transitions `unknown` records older than 120s to `failed`, or
2. Add `unknown` to the pruning logic with a longer TTL (e.g., 24h) as a safety net.

Additionally:

- Add a startup sweep that resolves any stale `unknown` records left from previous daemon runs.
- Consider whether `isRemoteSpawnPending` should treat `unknown` as non-pending to prevent infinite polling.

## Acceptance Criteria

- [ ] `unknown` records eventually transition to a terminal state (either via reconciliation timeout or pruning).
- [ ] `unknown` records do not accumulate indefinitely.
- [ ] Users are not stuck in an infinite poll loop when a spawn enters `unknown` state.
- [ ] Startup sweep cleans up stale `unknown` records from previous daemon runs.
- [ ] Tests verify the timeout/pruning behavior for `unknown` state.

## Work Log

- (none yet)
