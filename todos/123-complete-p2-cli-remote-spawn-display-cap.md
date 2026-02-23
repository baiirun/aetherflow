---
status: pending
priority: p2
issue_id: "123"
tags: [code-review, ux, tigerstyle, explicit-limits]
dependencies: []
---

# Cap remote spawn CLI display at N rows

## Problem Statement

The CLI remote spawn display iterates over all `s.RemoteSpawns` without a cap. The store allows up to 512 records (including terminal records within the retention window). With 512 entries, the terminal output would be ~512 lines — overwhelming and unusable. Pool agents are naturally bounded by `PoolSize`, but remote spawns accumulate.

## Findings

- `cmd/af/cmd/status.go:307` — `for _, rs := range s.RemoteSpawns {` with no limit
- `internal/daemon/remote_spawn_store.go:19` — `remoteSpawnMaxRecords = 512`
- The summary line (running/pending/terminal counts) is already shown before the rows
- `--json` output should still return all records (agents need them)
- Flagged by: tigerstyle-reviewer

## Proposed Solutions

### Option 1: Cap display at 50 rows with overflow message

**Approach:** Display first 50 remote spawns, then a dim "... and N more (use --json for full list)" message.

```go
const maxRemoteSpawnDisplay = 50
displayed := s.RemoteSpawns
if len(displayed) > maxRemoteSpawnDisplay {
    displayed = displayed[:maxRemoteSpawnDisplay]
}
for _, rs := range displayed { ... }
if len(s.RemoteSpawns) > maxRemoteSpawnDisplay {
    fmt.Printf("  %s\n", term.Dimf("... and %d more", len(s.RemoteSpawns)-maxRemoteSpawnDisplay))
}
```

**Pros:**
- Bounded terminal output
- Summary line still shows full counts
- `--json` unaffected

**Cons:**
- Magic number 50 needs justification

**Effort:** 10 minutes
**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `cmd/af/cmd/status.go:305-333` — remote spawn display loop

## Acceptance Criteria

- [ ] Terminal display capped at N rows
- [ ] Overflow message shown when truncated
- [ ] `--json` still returns all records
- [ ] Summary counts (running/pending/terminal) still accurate

## Work Log

### 2026-02-22 - Code Review Round 5

**By:** Claude Code

**Actions:**
- TigerStyle reviewer flagged unbounded display loop
