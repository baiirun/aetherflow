---
status: pending
priority: p3
issue_id: "093"
tags: [code-review, security]
dependencies: []
---

# Sanitize `LastError` before displaying to CLI users

## Problem Statement

`LastError` is populated from raw `err.Error()` from the Sprites provider HTTP client. Provider errors can contain internal infrastructure details (hostnames, IP addresses, internal paths, stack traces). This string is passed through to both stderr and JSON output without sanitization.

## Findings

- `cmd/af/cmd/sessions.go:480` — `rs.LastError` passed to error output
- `cmd/af/cmd/spawn.go:204,214` — `err.Error()` from provider stored directly in `rec.LastError`

## Proposed Solutions

### Option 1: Show sanitized summary, keep raw error in store file

**Approach:** Display `"spawn X is failed (see remote_spawns.json for details)"` instead of the raw error. Full error remains in the store file for debugging.

**Effort:** 15 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Provider error details not shown in CLI output
- [ ] Full error still available in remote_spawns.json for debugging

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified potential information leakage through provider error strings
