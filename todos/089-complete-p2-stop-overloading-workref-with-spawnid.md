---
status: pending
priority: p2
issue_id: "089"
tags: [code-review, correctness]
dependencies: []
---

# Stop overloading `WorkRef` with `SpawnID` for remote spawn entries

## Problem Statement

When constructing `sessions.Record` for remote spawn entries, `WorkRef` is set to `rs.SpawnID`. But `WorkRef` has established semantics — it's a task/work reference (e.g., `ts-123`). Overloading it with a spawn ID means:
- The WORK column shows spawn IDs instead of work references
- `sessionWhatForRecord` falls back to `WorkRef` and displays spawn_id as a "work ref"
- Future logic filtering by `WorkRef` will conflate spawn IDs with actual work references

## Findings

- `cmd/af/cmd/sessions.go:222` — `WorkRef: rs.SpawnID`
- `SpawnID` is already a first-class field on `sessionListEntry` and displayed via `sessionWhatForEntry`
- The WORK column and WHAT column both end up showing the spawn ID through different paths

## Proposed Solutions

### Option 1: Set WorkRef to empty for remote spawn entries

**Approach:** Change `WorkRef: rs.SpawnID` to `WorkRef: ""`. The spawn ID is already available via `sessionListEntry.SpawnID` and displayed through `sessionWhatForEntry`.

**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `WorkRef` is empty for remote-spawn-only entries
- [ ] WORK column shows `-` for remote spawns (no redundant spawn ID)
- [ ] WHAT column still shows spawn ID via `sessionWhatForEntry`

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified semantic overloading of WorkRef field
