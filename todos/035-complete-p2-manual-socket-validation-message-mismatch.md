---
status: complete
priority: p2
issue_id: "035"
tags: [code-review, daemon, configuration, ux]
dependencies: []
---

# Clarify manual socket validation contract

## Problem Statement

Manual mode without a project now requires an explicit socket path, but validation rejects the default socket path while the error text says socket-path is only "required." This is confusing and can mislead operators.

## Findings

- `internal/daemon/config.go:163` rejects manual mode when socket resolves to `protocol.DefaultSocketPath`.
- `internal/daemon/config.go:165` says socket-path is required, not that it must be non-default.
- Current behavior can fail even when users provide `--socket` set to default path.

## Proposed Solutions

### Option 1: Keep restriction and update wording/docs

**Approach:** Keep non-default requirement and update error/help/docs to state "must be non-default socket path".

**Pros:**
- Preserves isolation guardrail intent.
- Small and low-risk change.

**Cons:**
- Slightly stricter than typical defaults.

**Effort:** Small

**Risk:** Low

---

### Option 2: Allow explicit default socket when provided

**Approach:** Treat explicitly provided default socket as intentional and permit it.

**Pros:**
- Less surprising behavior.
- Fewer validation edge cases.

**Cons:**
- Re-opens accidental cross-daemon collision risk.

**Effort:** Small

**Risk:** Medium

## Recommended Action

Adopt Option 2 (allow default socket in manual mode) and rely on daemon bind failure to enforce single-instance behavior on shared default socket.
## Technical Details

- Affected files: `internal/daemon/config.go`, `README.md`, `cmd/af/cmd/daemon.go`
- Components: config validation and operator messaging
- Database changes: No

## Resources

- Review target: commit `006131715aed57dedad6bda8871350e5401f8816`

## Acceptance Criteria

- [x] Validation contract updated to allow manual mode without explicit socket.
- [x] CLI and README wording reflect singleton default-socket behavior.
- [x] Regression tests cover manual mode without project/socket and second-daemon fail-fast behavior.

## Work Log

### 2026-02-16 - Initial review finding

**By:** Claude Code

**Actions:**
- Consolidated reviewer findings about socket-path contract mismatch.
- Captured impacted files and options.

**Learnings:**
- Isolation guard is good, but language precision matters for operability.

### 2026-02-16 - Resolved with singleton default-socket policy

**By:** Claude Code

**Actions:**
- Removed manual-mode validation that required explicit `socket_path` when project is empty.
- Updated daemon startup hint and README docs to reflect manual default-socket behavior.
- Added integration test to assert second daemon on same socket fails fast with clear error.

**Learnings:**
- Product intent is better served by simple singleton semantics than by forcing explicit socket configuration.
