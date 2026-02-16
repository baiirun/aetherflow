---
status: complete
priority: p2
issue_id: "024"
tags: [code-review, safety]
dependencies: []
---

# Use separate timeout contexts for ParseToolCalls and ParseSessionID in buildSpawnDetail

## Problem Statement

`buildSpawnDetail` creates one 5-second timeout context shared between `ParseToolCalls` and `ParseSessionID`. Since they run sequentially, if `ParseToolCalls` takes 4.9s, `ParseSessionID` gets 100ms. Each operation should have its own timeout bound.

## Findings

- Found by: code-reviewer, tigerstyle-reviewer
- Location: `internal/daemon/status.go:301-318` (buildSpawnDetail)
- Pool agent path runs these concurrently in goroutines with the same context — same issue but less severe since they run in parallel

## Proposed Solutions

### Option 1: Separate timeout per operation (Recommended)

```go
toolCtx, toolCancel := context.WithTimeout(ctx, 5*time.Second)
defer toolCancel()
calls, skipped, err := ParseToolCalls(toolCtx, entry.LogPath, limit)

sessCtx, sessCancel := context.WithTimeout(ctx, 2*time.Second)
defer sessCancel()
sessionID, err := ParseSessionID(sessCtx, entry.LogPath)
```

- **Pros:** Each operation gets its full timeout
- **Cons:** Total timeout now up to 7s instead of 5s
- **Effort:** Tiny
- **Risk:** Low

## Technical Details

- **Affected files:** `internal/daemon/status.go`

## Acceptance Criteria

- [ ] Each parse operation has its own timeout context
- [ ] No regression in status detail behavior

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | |
