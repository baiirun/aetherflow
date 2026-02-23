---
status: pending
priority: p2
issue_id: "088"
tags: [code-review, testing]
dependencies: ["087"]
---

# Add test for `buildSessionListEntries` merge/dedup/sort logic

## Problem Statement

The core Phase 4 function — merging session records with remote spawn records, deduplicating by session_id, applying server filter, and sorting — has zero direct test coverage. This is the most complex and bug-prone part of the change, yet only the leaf functions (`remoteSpawnStatusToSessionStatus`, `sessionWhatForEntry`) are tested.

## Findings

- `cmd/af/cmd/sessions_test.go` — no test for `buildSessionListEntries`
- Blocked by #087 (cobra coupling makes it untestable)
- Dedup logic (seenSessionIDs), server filter on remote spawns, and sort order are all untested
- Reported by: code-reviewer, simplicity-reviewer, tigerstyle-reviewer

## Proposed Solutions

### Option 1: Table-driven test after #087 refactor

**Approach:** After decoupling from cobra (#087), add a table-driven test covering:
1. Remote spawn with session_id matching existing session → deduplicated
2. Remote spawn with no matching session → appears in output
3. Server filter applied to remote spawns (both match and no-match)
4. Sort order of mixed entries
5. Empty remote spawn list (graceful degradation)
6. Remote spawn with empty ServerRef and active filter

**Effort:** 30 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Test covers dedup by session_id
- [ ] Test covers server filter for remote spawns
- [ ] Test covers sort order
- [ ] Test covers empty remote spawn list
- [ ] `go test ./cmd/af/cmd/ -run TestBuildSessionListEntries` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified missing test coverage for core merge function
- Blocked by #087 (cobra coupling)
