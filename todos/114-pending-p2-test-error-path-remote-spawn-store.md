---
status: pending
priority: p2
issue_id: "114"
tags: [code-review, testing, error-handling]
dependencies: []
---

# Add test for BuildFullStatus when RemoteSpawnStore.List() fails

## Problem Statement

`BuildFullStatus` captures `RSpawns.List()` errors into `status.Errors` (status.go:185-186), but no test exercises this path. A corrupted JSON file or permission error would hit this. Per the codebase's testing philosophy, error handling code should be tested.

Flagged by tigerstyle-reviewer and grug-brain-reviewer.

## Findings

- `internal/daemon/status.go:185-186` — error captured in status.Errors
- No existing test covers `RSpawns.List()` returning an error
- The error path is simple (one append) but regression-worthy

## Proposed Solutions

### Option 1: Test with corrupted JSON file

**Approach:** Create a store pointing at a directory with a corrupted `remote_spawns.json`, then call `BuildFullStatus` and assert error is captured.

```go
func TestBuildFullStatusRemoteSpawnStoreError(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "remote_spawns.json"), []byte("{corrupt"), 0o600)
    rspawns, _ := OpenRemoteSpawnStore(dir)
    
    status := BuildFullStatus(ctx, StatusSources{RSpawns: rspawns}, cfg, nil)
    
    if len(status.Errors) == 0 {
        t.Fatal("expected error from corrupted store")
    }
    if len(status.RemoteSpawns) != 0 {
        t.Error("expected no remote spawns on error")
    }
}
```

**Pros:** Tests the real error path, no mocking
**Cons:** None
**Effort:** 15 minutes
**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status_test.go` — add test

## Acceptance Criteria

- [ ] Test exercises `RSpawns.List()` error path in `BuildFullStatus`
- [ ] Test asserts error appears in `status.Errors`
- [ ] Test asserts `status.RemoteSpawns` is empty on error
- [ ] Tests pass

## Work Log

### 2026-02-22 - Initial Discovery

**By:** Claude Code (code review)

**Actions:**
- Identified untested error path in BuildFullStatus
- Drafted test approach using corrupted JSON file
