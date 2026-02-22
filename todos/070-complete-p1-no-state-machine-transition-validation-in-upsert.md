---
status: pending
priority: p1
issue_id: "070"
tags: [code-review, correctness, data-integrity]
dependencies: []
---

# No state machine transition validation in Upsert

## Problem Statement

`internal/daemon/remote_spawn_store.go:88-148` — `Upsert` accepts any state transition silently. Terminal states (`failed`, `terminated`) can be reversed. The plan defines explicit allowed transitions but the code doesn't enforce them.

## Findings

- Code-reviewer, tigerstyle-reviewer, and data-integrity-guardian all flagged this independently.
- `Upsert` overwrites state unconditionally when updating existing records, with no validation against the defined state machine.
- Terminal states like `failed` and `terminated` can be reversed to any earlier state (e.g., `failed→requested`), violating the plan's invariants.
- This undermines the reliability of state-dependent logic throughout the system.

## Proposed Solution

1. Define a `validTransitions` map matching the plan's state machine (e.g., `requested→pending→running→terminated`, `*→failed` but not `failed→*`).
2. In `Upsert`, when updating an existing record, check the current state against `validTransitions` before applying the new state.
3. Return an error for invalid transitions instead of silently accepting them.

**Effort:** Small (30 min)

## Acceptance Criteria

- [ ] A `validTransitions` map is defined matching the plan's state machine.
- [ ] `Upsert` validates state transitions when updating existing records and returns an error for invalid transitions.
- [ ] Invalid transitions (e.g., `failed→requested`) return an error.
- [ ] Test covers all valid and invalid transition pairs from the state machine.

## Work Log
