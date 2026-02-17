---
status: complete
priority: p2
issue_id: "036"
tags: [code-review, daemon, testing, reliability]
dependencies: []
---

# Make auto policy runtime test assert loop behavior

## Problem Statement

`TestDaemonAutoPolicyUsesRunnerCalls` can pass due to status RPC calls rather than daemon background scheduling loops. This weakens regression protection for spawn-policy behavior.

## Findings

- `internal/daemon/daemon_manual_test.go:107` calls `waitForDaemonStatus`, which triggers `status.full`.
- In auto mode, `status.full` itself can execute prog calls, incrementing the runner count.
- Assertion at `internal/daemon/daemon_manual_test.go:117` checks only `calls > 0`.

## Proposed Solutions

### Option 1: Track command types and assert loop-specific calls

**Approach:** Record invoked command/args and assert periodic `prog ready` (or equivalent loop-originated calls) after baseline.

**Pros:**
- Strong proof of runtime loop behavior.
- Resistant to false positives from status RPC.

**Cons:**
- Slightly more test harness code.

**Effort:** Medium

**Risk:** Low

---

### Option 2: Baseline call counter after readiness, then assert growth

**Approach:** Capture counter immediately after first ready status call, wait > poll interval, assert counter increases from loop activity.

**Pros:**
- Minimal code changes.

**Cons:**
- Still partially timing-sensitive.

**Effort:** Small

**Risk:** Medium

## Recommended Action

Use Option 1: track command type and assert `prog ready` call growth after status baseline.

## Technical Details

- Affected files: `internal/daemon/daemon_manual_test.go`
- Components: daemon runtime policy regression tests
- Database changes: No

## Resources

- Review target: commit `006131715aed57dedad6bda8871350e5401f8816`

## Acceptance Criteria

- [x] Auto-mode test asserts loop-originated `prog ready` activity after readiness baseline.
- [x] Manual-mode test still proves zero runner calls.
- [x] Test no longer relies on `calls > 0` from status RPC path.

## Work Log

### 2026-02-16 - Initial review finding

**By:** Claude Code

**Actions:**
- Captured false-positive path in auto-mode daemon policy test.

**Learnings:**
- Counter-based assertions need command provenance for robust loop coverage.

### 2026-02-16 - Resolved

**By:** Claude Code

**Actions:**
- Updated `TestDaemonAutoPolicyUsesRunnerCalls` to count `prog ready` invocations separately.
- Captured a baseline immediately after readiness/status RPC and asserted `prog ready` increases afterward.
- Preserved total-call counter for debug context in failure messages.

**Learnings:**
- Baseline + command-specific counters prevents false positives from status-triggered runner calls.
