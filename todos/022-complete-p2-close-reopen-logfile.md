---
status: complete
priority: p2
issue_id: "022"
tags: [code-review, quality, simplicity]
dependencies: []
---

# Fix close-then-reopen log file pattern in detached mode

## Problem Statement

`runSpawn` opens the log file unconditionally, then passes it to both `runForeground` and `runDetached`. `runDetached` immediately closes the file and opens a new handle to the same path. This is confusing — the caller creates a resource that one callee immediately discards.

## Findings

- Found by: code-simplicity-reviewer, pattern-recognition-specialist, tigerstyle-reviewer
- Location: `cmd/af/cmd/spawn.go:95` (open), `spawn.go:186` (close), `spawn.go:196` (reopen)
- `runForeground` actually uses the file handle; `runDetached` doesn't

## Proposed Solutions

### Option 1: Only open for foreground path (Recommended)

**Approach:** Move log file creation into `runForeground`. Pass only `logPath` to `runDetached`.

```go
if detach {
    runDetached(spawnID, userPrompt, spawnCmdStr, prompt, logPath, socketPath)
    return
}
logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
if err != nil { Fatal(...) }
runForeground(spawnID, userPrompt, spawnCmdStr, prompt, logFile, logPath, socketPath)
```

- **Pros:** Clear ownership, no wasted open/close, simpler signatures
- **Cons:** Minor restructure
- **Effort:** Small
- **Risk:** Low

## Technical Details

- **Affected files:** `cmd/af/cmd/spawn.go`

## Acceptance Criteria

- [ ] `runDetached` doesn't receive or close a pre-opened file
- [ ] Log file only opened when needed (foreground path)
- [ ] Both paths still produce correct log files

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | Found by 3 reviewers — clear consensus |
