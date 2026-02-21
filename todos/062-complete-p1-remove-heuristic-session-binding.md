---
status: complete
priority: p1
issue_id: "062"
tags: [code-review, correctness, safety]
dependencies: []
---

# Remove heuristic session binding (tryBindRemoteSpawnSession)

## Problem Statement

`tryBindRemoteSpawnSession()` uses a heuristic approach that matches sessions to spawns based on "same `server_ref` + exactly one unclaimed session". This can bind the **wrong** session to a spawn because there's no verification that the session actually corresponds to this spawn's `spawn_id`/`request_id`.

Race condition: read-then-write (TOCTOU) with no uniqueness guard on `session_id` in the store means concurrent attach calls can double-assign one session to multiple spawns.

**All 10 reviewers flagged this as P1.**

## Findings

- Heuristic binding in `cmd/af/cmd/sessions.go:471` matches by server_ref elimination, not correlation
- No metadata verification — any unclaimed session on the same server gets bound
- TOCTOU race: concurrent callers can both read "1 candidate" and bind the same session
- The `goto resolved` pattern adds unnecessary complexity to the attach flow

## Recommended Action

Remove `tryBindRemoteSpawnSession()` entirely. Keep returning `SESSION_NOT_READY` (exit 3) for pending spawns. Session binding should only happen through explicit correlation from the daemon reconcile loop or provider plugin events — never from client-side heuristics.

## Technical Details

**Affected files:**
- `cmd/af/cmd/sessions.go` — remove function and all call sites
- `cmd/af/cmd/sessions_test.go` — remove heuristic binding tests

## Acceptance Criteria

- [ ] `tryBindRemoteSpawnSession` function removed
- [ ] All call sites removed from attach flow
- [ ] `goto resolved` label removed
- [ ] Heuristic binding tests removed
- [ ] Pending spawns always return SESSION_NOT_READY until explicit binding
