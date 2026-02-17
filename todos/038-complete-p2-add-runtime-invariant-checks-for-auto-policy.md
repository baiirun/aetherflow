---
status: complete
priority: p2
issue_id: "038"
tags: [code-review, reliability, daemon, architecture]
dependencies: []
---

# Assert auto policy invariants in daemon run path

## Problem Statement

Daemon runtime assumes config was pre-validated. If validation is bypassed, auto mode can start RPC serving without poller/pool loops, creating silent degraded behavior.

## Findings

- `internal/daemon/daemon.go:119` gates loop startup with nil checks.
- No explicit runtime assertion enforces `auto => poller and pool must exist`.
- This can fail open in non-CLI construction paths.

## Proposed Solutions

### Option 1: Fail fast with explicit invariants in `Run`

**Approach:** Add runtime checks for policy preconditions and return clear errors on invariant violations.

**Pros:**
- Prevents silent degraded mode.
- Easier troubleshooting and safer behavior.

**Cons:**
- Adds explicit startup failure path.

**Effort:** Small

**Risk:** Low

---

### Option 2: Validate in constructor and store normalized config only

**Approach:** Move validation responsibility into `New` and reject invalid configs earlier.

**Pros:**
- Centralized contract enforcement.

**Cons:**
- API behavior change for existing callers.

**Effort:** Medium

**Risk:** Medium

## Recommended Action

Implemented Option 1: fail fast in `Run` with explicit runtime invariant checks.

## Technical Details

- Affected files: `internal/daemon/daemon.go`, maybe `internal/daemon/config.go`
- Components: daemon lifecycle and policy gating
- Database changes: No

## Resources

- Review target: commit `006131715aed57dedad6bda8871350e5401f8816`

## Acceptance Criteria

- [x] Auto policy startup fails with clear error if required runtime dependencies are missing.
- [x] Existing valid startup paths remain unchanged.
- [x] Regression tests cover invariant failure cases.

## Work Log

### 2026-02-16 - Initial review finding

**By:** Claude Code

**Actions:**
- Documented fail-open startup risk and defensive fix options.

**Learnings:**
- Strong config validation exists, but runtime assertion gives safer negative-space guarantee.

### 2026-02-16 - Resolved

**By:** Claude Code

**Actions:**
- Added explicit runtime checks in `Daemon.Run` for:
  - unknown spawn-policy
  - `auto` policy without project
  - `auto` policy missing poller/pool
  - poller/pool asymmetry
- Added regression tests asserting `Run` errors for auto-without-project and unknown policy.

**Learnings:**
- Runtime assertions provide a defensive backstop when non-CLI callers bypass validation.
