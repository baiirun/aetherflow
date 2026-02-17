---
status: complete
priority: p3
issue_id: "040"
tags: [code-review, security, daemon, filesystem]
dependencies: []
---

# Harden socket file deletion and path safety checks

## Problem Statement

Daemon startup removes the configured socket path before binding. With manual mode relying more on user-provided socket paths, path safety checks should be stricter.

## Findings

- `internal/daemon/daemon.go:86` unconditionally calls `os.Remove(d.config.SocketPath)`.
- Validation currently focuses on policy/project combinations, not path ownership/type constraints.
- Attack surface is local but avoidable with stronger checks.

## Proposed Solutions

### Option 1: Remove only if target is an existing Unix socket

**Approach:** `Lstat` path and remove only when mode indicates socket; reject other file types.

**Pros:**
- Prevents accidental deletion of non-socket files.
- Minimal behavior change.

**Cons:**
- Adds a bit of startup code complexity.

**Effort:** Small

**Risk:** Low

---

### Option 2: Also enforce secure parent directory rules

**Approach:** Require absolute path and validate parent directory ownership/permissions.

**Pros:**
- Stronger local hardening.

**Cons:**
- Can reject some existing setups.

**Effort:** Medium

**Risk:** Medium

## Recommended Action

Implemented Option 1: only remove stale socket files and reject non-socket path collisions.

## Technical Details

- Affected files: `internal/daemon/daemon.go`, `internal/daemon/config.go`
- Components: socket setup and filesystem safety
- Database changes: No

## Resources

- Review target: commit `006131715aed57dedad6bda8871350e5401f8816`

## Acceptance Criteria

- [x] Daemon removes only socket files at target path.
- [x] Non-socket path collisions fail with clear errors.
- [x] Socket path hardening tests cover non-socket collision edge case.

## Work Log

### 2026-02-16 - Initial review finding

**By:** Claude Code

**Actions:**
- Documented low-severity path hardening gap from security review.

**Learnings:**
- New manual-mode workflows increase value of explicit filesystem checks.

### 2026-02-16 - Partial mitigation landed

**By:** Claude Code

**Actions:**
- Added startup pre-check to detect an already-running daemon on the target socket and fail fast with `daemon already running on <socket>` before stale-socket removal.
- Added integration test covering second-daemon startup failure on same socket.

**Learnings:**
- Singleton behavior is now explicit and user-friendly, but type-safe stale socket cleanup (`Lstat`/socket-only remove) is still pending.

### 2026-02-16 - Completed path-type hardening

**By:** Claude Code

**Actions:**
- Added `Lstat` guard before stale-socket removal.
- Return explicit error when configured socket path exists but is not a Unix socket.
- Remove stale path only when existing node is a Unix socket.
- Added daemon regression test for non-socket path collision.

**Learnings:**
- Socket-type checks provide low-cost safety hardening without changing normal startup behavior.
