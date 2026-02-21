---
status: complete
priority: p2
issue_id: "057"
tags: [code-review, security, trust-policy]
dependencies: []
---

# Harden trusted host policy for remote attach targets

## Problem Statement

Remote HTTPS attach targets are accepted broadly; this expands trust surface and may allow registry/provider URL tampering to redirect attach traffic.

## Findings

- `internal/daemon/server_url.go:42` allows remote HTTPS hosts without domain allowlist policy.
- `cmd/af/cmd/sessions.go:365` passes validated `server_ref` directly to `opencode attach`.

## Proposed Solutions

### Option 1: Add host allowlist for remote attach

**Approach:** Require remote hosts to match configured trusted domains (e.g., `*.sprites.app`).

**Pros:** Strong trust boundary.

**Cons:** Needs config UX.

**Effort:** 3-5 hours

**Risk:** Medium

### Option 2: Validate on write + use

**Approach:** Enforce trust policy when persisting and when attaching.

**Pros:** Defense in depth.

**Cons:** More code paths.

**Effort:** 4-6 hours

**Risk:** Medium

## Recommended Action

Start with Option 1 and apply Option 2 for defense in depth.

## Technical Details

**Affected files:**
- `internal/daemon/server_url.go`
- `cmd/af/cmd/sessions.go`
- `internal/daemon/config.go`

## Acceptance Criteria

- [ ] remote attach host must pass explicit trust policy
- [ ] trust policy failures return stable error code
- [ ] tests cover allow and deny host cases

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Logged trust-boundary finding from security reviewer.
