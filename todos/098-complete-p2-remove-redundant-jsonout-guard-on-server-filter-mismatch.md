---
status: pending
priority: p2
issue_id: "098"
tags: [code-review, quality, dead-code]
dependencies: []
---

# Remove redundant `if jsonOut` guard on SERVER_FILTER_MISMATCH

## Problem Statement

In `runSessionAttach`, the `SERVER_FILTER_MISMATCH` error path wraps `handleAttachError` in an `if jsonOut` check, but `handleAttachError` already branches on `jsonOut` internally. This is a leftover from the old two-function pattern and means:

1. The error code `SERVER_FILTER_MISMATCH` is never surfaced in the non-JSON stderr path
2. The spawn context (State, SpawnID) is lost in the non-JSON path
3. It's the only call site with this redundant guard — inconsistent with all other call sites

## Findings

- `cmd/af/cmd/sessions.go:472-477` — the `if jsonOut` guard is dead logic after the `handleAttachError`/`handleAttachErrorWithSpawn` merge (#094)
- All other call sites pass `jsonOut` through and let `handleAttachError` decide the output format
- Non-JSON users get "spawn not found for server" instead of the more accurate "SERVER_FILTER_MISMATCH: spawn X maps to server Y, not Z"

## Proposed Solutions

### Option 1: Remove the `if jsonOut` guard (Recommended)

**Approach:** Call `handleAttachError` unconditionally and `os.Exit(1)` after, removing the `Fatal` fallthrough.

```go
if serverFilter != "" && rs.ServerRef != "" && rs.ServerRef != serverFilter {
    handleAttachError(jsonOut, "SERVER_FILTER_MISMATCH", rs, fmt.Errorf("spawn %s maps to server %s, not %s", rs.SpawnID, rs.ServerRef, serverFilter))
    os.Exit(1)
}
```

**Pros:** Consistent with all other call sites; surfaces accurate error info in both modes
**Cons:** None
**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `handleAttachError` called without `if jsonOut` guard
- [ ] `Fatal` fallthrough removed
- [ ] Both JSON and non-JSON paths show SERVER_FILTER_MISMATCH code and spawn context
- [ ] `go build ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified during Phase 4 review by code-reviewer agent
- Leftover from #094 merge of handleAttachError/handleAttachErrorWithSpawn
