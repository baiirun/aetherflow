---
status: pending
priority: p2
issue_id: "092"
tags: [code-review, security]
dependencies: []
---

# Validate `SessionID` doesn't start with `-` before `exec.Command`

## Problem Statement

Before executing `opencode attach`, the code validates `ServerRef` against the trusted host list and checks for `-` prefix (argument injection). But `SessionID` is not checked — a malicious session ID like `--help` or `--config /etc/passwd` passed as `--session` argument could be interpreted as a flag by `opencode`.

For remote spawns, `SessionID` comes from provider-returned data — a different trust zone than local session records.

## Findings

- `cmd/af/cmd/sessions.go:506-512` — `ServerRef` validated, `SessionID` not checked
- `SessionID` flows from `RemoteSpawnStore` (provider data) into `exec.Command` args
- `exec.Command("opencode", "attach", target.ServerRef, "--session", target.SessionID)` at line 512

## Proposed Solutions

### Option 1: Add `-` prefix check for SessionID

**Approach:** Add the same prefix check before `exec.Command`:
```go
if strings.HasPrefix(target.SessionID, "-") {
    Fatal("invalid session_id %q in session registry", target.SessionID)
}
```

**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] SessionID starting with `-` is rejected before exec
- [ ] Test added for this validation

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified missing argument injection check on SessionID
