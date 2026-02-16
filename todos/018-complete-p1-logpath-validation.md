---
status: complete
priority: p1
issue_id: "018"
tags: [code-review, security]
dependencies: []
---

# Validate LogPath in spawn.register to prevent path traversal

## Problem Statement

The `LogPath` field in `SpawnRegisterParams` is stored verbatim and later read by the daemon via `ParseToolCalls` and `ParseSessionID` in `buildSpawnDetail`. A malicious registration could point `LogPath` at any file readable by the daemon user, and the daemon would attempt to parse it as JSONL and return parsed content in the `status.agent` response.

Pool agents derive log paths server-side via `logFilePath()` which uses `filepath.Base(taskID)`. Spawned agents bypass this protection entirely.

## Findings

- Found by: security-sentinel
- Location: `internal/daemon/spawn_rpc.go:35` (stored), `internal/daemon/logs.go:55-61` (returned), `internal/daemon/status.go:304,314` (read)
- `logFilePath()` in existing code uses `filepath.Base` to prevent traversal — spawn bypasses this

## Proposed Solutions

### Option 1: Validate LogPath is under LogDir (Recommended)

**Approach:** Validate that the cleaned log path is under the configured log directory.

```go
clean := filepath.Clean(params.LogPath)
rel, err := filepath.Rel(cfg.LogDir, clean)
if err != nil || strings.HasPrefix(rel, "..") {
    return &Response{Success: false, Error: "log_path must be under log directory"}
}
```

- **Pros:** Simple, matches pool pattern
- **Cons:** Requires LogDir to be available in the RPC handler
- **Effort:** Small
- **Risk:** Low

### Option 2: Derive LogPath server-side from SpawnID

**Approach:** Don't accept LogPath from the client. Derive it as `filepath.Join(cfg.LogDir, spawnID+".jsonl")` — same pattern as pool agents.

- **Pros:** Eliminates the attack surface entirely; matches pool pattern exactly
- **Cons:** Client can't customize log location; CLI must use daemon's log dir
- **Effort:** Small (remove LogPath from params, derive in handler)
- **Risk:** Low — but changes the API contract

## Recommended Action

Option 2 — derive server-side. The CLI already generates the log path using the same pattern (`filepath.Join(logDir, spawnID+".jsonl")`), so there's no loss of flexibility. This eliminates the entire attack surface.

## Technical Details

- **Affected files:** `spawn_rpc.go`, `client/client.go` (remove LogPath from params), `spawn.go` (remove LogPath from register call)
- **Components:** handleSpawnRegister, SpawnRegisterParams, registerSpawn

## Acceptance Criteria

- [ ] LogPath is derived server-side from SpawnID + LogDir
- [ ] LogPath field removed from SpawnRegisterParams
- [ ] Test that registration produces correct log path
- [ ] buildSpawnDetail uses the derived path

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | Pool agents already derive paths server-side — spawn should match |

## Resources

- Existing pattern: `logFilePath()` in logs.go uses `filepath.Base`
