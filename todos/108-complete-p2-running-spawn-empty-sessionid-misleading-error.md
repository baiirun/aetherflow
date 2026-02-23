---
status: pending
priority: p2
issue_id: "108"
tags: [code-review, ux, edge-case]
dependencies: []
---

# Running spawn with empty SessionID gives misleading "not found" error

## Problem Statement

In `runSessionAttach`, if a remote spawn is in `running` state but hasn't reported its `SessionID` yet (narrow timing window), none of the three conditions match (not pending, not terminal, no session_id to append), so `matches` stays empty and the user gets `"session or spawn not found"`. The user has no idea the spawn is actually running — they just need to wait.

## Findings

- `cmd/af/cmd/sessions.go:479-491` — the three conditions (pending, terminal+no-session, has-session) don't cover running+no-session
- This is a narrow timing window between the spawn reaching `running` state and the plugin reporting the session_id back
- The user's correct action is to retry, but the "not found" message doesn't suggest that

## Proposed Solutions

### Option 1: Treat running+empty-SessionID as pending (Recommended)

**Approach:** Extend the pending check to include running spawns that haven't reported a session_id yet.

```go
if daemon.IsRemoteSpawnPending(rs) || (rs.State == daemon.RemoteSpawnRunning && rs.SessionID == "") {
    handleAttachPending(jsonOut, rs)
    os.Exit(3)
}
```

**Pros:** Gives user the retry hint; accurate representation of the situation
**Cons:** None
**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Running spawn with empty SessionID returns SESSION_NOT_READY with retry hint
- [ ] Running spawn with populated SessionID still attaches normally
- [ ] `go test ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified by code-reviewer agent during round 3 review
- Narrow race condition in the attach flow
