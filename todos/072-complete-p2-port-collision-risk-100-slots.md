---
status: pending
priority: p2
issue_id: "072"
tags: [code-review, correctness]
dependencies: []
---

# Port collision risk — 100 slots with no detection

## Problem Statement

`internal/protocol/socket.go:24` — `simpleHash(project)%100` gives only 100 port slots (7071-7170). Birthday paradox: ~10% collision at 5 projects, ~37% at 10. Two projects silently fight over the same port with confusing "daemon already running" errors.

## Findings

- The hash function maps project names into a 100-slot range, which is far too small for real-world usage.
- Birthday paradox probabilities mean collisions become likely with only a handful of projects.
- When a collision occurs, a second project attempting to start a daemon receives a misleading "daemon already running" error because another project's daemon already occupies that port.
- No mechanism exists to detect or resolve these collisions.

## Proposed Solution

Either:

1. Expand the range to 1000 slots (`%1000`, ports 7071-8070) to reduce collision probability significantly, or
2. Detect collisions at startup by checking if the existing daemon on the assigned port belongs to a different project, and if so, pick an alternative port or report a clear error.

Option 2 is more robust but requires more work. Option 1 is a quick mitigation.

## Acceptance Criteria

- [ ] Port collision probability is reduced to acceptable levels (< 1% at 10 projects).
- [ ] If collision detection is implemented: startup detects when an existing daemon on the port belongs to a different project and either reassigns or reports a clear error.
- [ ] Existing daemon lookup behavior is not broken by the change.
- [ ] Test covers the collision scenario (two different project names mapping to the same port).

## Work Log

- (none yet)
