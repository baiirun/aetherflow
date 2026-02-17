---
status: complete
priority: p2
issue_id: "031"
tags: [code-review, daemon, testing]
dependencies: []
---

# Add integration coverage for manual spawn policy runtime gating

## Problem Statement

The new spawn policy behavior is validated mostly through unit-level config/status tests, but there is no daemon-level test asserting that manual mode does not start poll/reclaim/reconcile loops. This is behavior-critical and can regress silently.

## Findings

- Manual gating logic lives in `internal/daemon/daemon.go:121-153`.
- Existing tests added in this change focus on config merge/validation and status serialization (`internal/daemon/config_test.go`, `internal/daemon/status_test.go`).
- No daemon run-path test verifies absence of `prog` calls or loop startup side effects in manual mode.

## Proposed Solutions

### Option 1: Add daemon run-path test with counting runner (recommended)

- Start daemon with `spawn_policy=manual` in test.
- Use a fake runner that increments counters per command.
- Assert no `prog ready`, `prog list`, or `git fetch` calls over a bounded interval.
- **Effort:** Medium
- **Risk:** Low

### Option 2: Extract startup orchestration and unit test branch selection

- Refactor startup loop wiring into a helper (`startAutoLoops`).
- Unit test that helper is called only in auto mode.
- **Effort:** Medium
- **Risk:** Medium (refactor churn)

### Option 3: Rely on logging assertions only

- Assert `spawn policy manual: auto-scheduling disabled` is emitted.
- Keep current test scope otherwise.
- **Effort:** Small
- **Risk:** High (does not prove loops are inactive)

## Recommended Action


## Technical Details

- Affected files: `internal/daemon/daemon.go`, new daemon runtime test file (for example `internal/daemon/daemon_manual_test.go`)
- Components: daemon startup orchestration and policy gating

## Acceptance Criteria

- [x] Test verifies manual mode performs zero poll/reclaim/reconcile command invocations.
- [x] Test verifies auto mode still performs expected scheduling/reconcile invocations.
- [x] Test is deterministic and bounded (no flaky timing assumptions).
- [x] Test fails if future changes accidentally re-enable auto loops in manual mode.

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from workflow review | The core policy branch is present, but loop-level behavior is not regression-tested at daemon runtime level. |
| 2026-02-16 | Added `internal/daemon/daemon_manual_test.go` with manual/auto runtime assertions | Manual policy now has daemon-level regression coverage for runner-call gating. |

## Resources

- PR/branch context: local working tree changes on `main`
