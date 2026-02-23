---
status: pending
priority: p2
issue_id: "099"
tags: [code-review, error-handling, observability]
dependencies: []
---

# Warn on remote spawn store List() error instead of silent swallow

## Problem Statement

In `runSessions`, both the remote spawn store open error and the `List()` error are silently discarded. If the store is corrupted or has permission issues, remote spawns silently disappear from the listing with no diagnostic output. Users would have no idea why their spawns aren't showing.

```go
if remoteStore, rsErr := openRemoteSpawnStore(cmd); rsErr == nil {
    remoteRecs, _ = remoteStore.List()
}
```

## Findings

- `cmd/af/cmd/sessions.go:128-130` — both errors swallowed
- Graceful degradation is the right behavior (listing should still work without remote spawns)
- But the `List()` error specifically could indicate data corruption or permission issues
- Consistent with `loadOpencodeSessionIndex()` (line 286) which also silently degrades — but that's a subprocess exec, not a local file read

## Proposed Solutions

### Option 1: Warn to stderr on List() failure (Recommended)

**Approach:** Keep graceful degradation but emit a warning.

```go
if remoteStore, rsErr := openRemoteSpawnStore(cmd); rsErr == nil {
    var listErr error
    remoteRecs, listErr = remoteStore.List()
    if listErr != nil {
        fmt.Fprintf(os.Stderr, "warning: reading remote spawn store: %v\n", listErr)
    }
}
```

**Pros:** Users see why spawns are missing; doesn't break listing
**Cons:** Adds a line of stderr output on failure
**Effort:** 5 minutes
**Risk:** Low

### Option 2: Log only when --verbose or debug mode

**Approach:** Only emit warning when verbose logging is enabled.

**Pros:** Cleaner default output
**Cons:** Requires --verbose flag infrastructure (may not exist yet)
**Effort:** 30 minutes if --verbose doesn't exist
**Risk:** Low

## Acceptance Criteria

- [ ] `List()` error produces a warning on stderr
- [ ] Listing still works when remote spawn store is unavailable
- [ ] `go test ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified by code-reviewer, simplicity-reviewer, and architecture-strategist agents
- All three independently flagged the silent error swallowing
