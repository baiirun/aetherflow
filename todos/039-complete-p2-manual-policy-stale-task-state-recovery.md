---
status: complete
priority: p2
issue_id: "039"
tags: [code-review, daemon, data-integrity, operations]
dependencies: []
---

# Add stale task state safeguards for manual mode

## Problem Statement

Manual spawn policy skips reclaim and reconcile loops. If operators switch to manual with existing in-progress or reviewing tasks, task state can remain stale indefinitely.

## Findings

- `internal/daemon/daemon.go:121` manual branch disables auto scheduling paths.
- Reclaim and reconcile are started only in auto branch.
- No explicit warning/recovery flow for stale task states during manual startup.

## Proposed Solutions

### Option 1: Manual-mode startup warning and one-shot recovery command

**Approach:** On manual startup, detect stale task states and print warning with explicit recovery command.

**Pros:**
- Reduces silent state drift.
- Minimal behavior change.

**Cons:**
- Requires operator action.

**Effort:** Medium

**Risk:** Low

---

### Option 2: Run one-time reclaim/reconcile before entering manual steady state

**Approach:** Perform safe one-shot cleanup at startup when project is set.

**Pros:**
- Automatic guardrail.

**Cons:**
- Manual mode may still touch prog unexpectedly.

**Effort:** Medium

**Risk:** Medium

## Recommended Action

Accepted as by-design behavior for manual mode: no automatic task lifecycle/recovery loops.

## Technical Details

- Affected files: `internal/daemon/daemon.go`, possibly CLI docs/commands
- Components: task state lifecycle and operational safeguards
- Database changes: No

## Resources

- Review target: commit `006131715aed57dedad6bda8871350e5401f8816`

## Acceptance Criteria

- [x] Product decision: manual mode is explicitly spawn-only and does not auto-recover task states.
- [x] Documentation updated to clarify poll/reclaim/reconcile are disabled in manual mode.
- [x] No runtime behavior change required.

## Work Log

### 2026-02-16 - Initial review finding

**By:** Claude Code

**Actions:**
- Captured state-consistency risk from disabling reclaim/reconcile in manual mode.

**Learnings:**
- Manual mode reduces background churn but needs explicit stale-state guardrails.

### 2026-02-16 - Closed by design decision

**By:** Claude Code

**Actions:**
- Aligned with product decision that manual mode opts out of automated task lifecycle management.
- Added explicit README note in Daemon Internals clarifying poll/reclaim/reconcile are disabled in manual mode.

**Learnings:**
- Clarifying intent in docs prevents future confusion without adding behavior that conflicts with manual-mode semantics.
