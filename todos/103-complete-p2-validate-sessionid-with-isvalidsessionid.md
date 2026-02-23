---
status: pending
priority: p2
issue_id: "103"
tags: [code-review, security, defense-in-depth]
dependencies: []
---

# Validate SessionID with isValidSessionID before exec.Command

## Problem Statement

The `SessionID` passed to `exec.Command` is only checked for a dash prefix. However, the codebase already has a strict `isValidSessionID()` function in `internal/daemon/spawn_cmd.go` that validates session IDs are `[a-zA-Z0-9_-]{1,128}`. This function is used in the spawn path but not in the attach path.

## Findings

- `cmd/af/cmd/sessions.go:518-520` — only dash prefix check on SessionID
- `internal/daemon/spawn_cmd.go:36-48` — `isValidSessionID()` with strict regex
- `exec.Command` passes args as discrete elements (no shell), so this isn't command injection — but malformed IDs could cause issues in `opencode attach`
- ServerRef already has `ValidateServerURLAttachTarget` for defense-in-depth; SessionID should have equivalent

## Proposed Solutions

### Option 1: Export and use IsValidSessionID (Recommended)

**Approach:** Export `isValidSessionID` as `IsValidSessionID` and use it in the attach path.

```go
if !daemon.IsValidSessionID(target.SessionID) {
    Fatal("invalid session_id %q in session registry", target.SessionID)
}
```

**Pros:** Defense-in-depth; consistent validation; reuses existing logic
**Cons:** Requires exporting a currently unexported function
**Effort:** 10 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `isValidSessionID` exported as `IsValidSessionID`
- [ ] Used in `runSessionAttach` before `exec.Command`
- [ ] Dash prefix check can be removed (subsumed by validation)
- [ ] `go test ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified by security-sentinel agent
