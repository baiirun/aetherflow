---
status: pending
priority: p2
issue_id: "009"
tags: [code-review, quality, dead-code]
dependencies: []
---

# Remove dead code: `logFileForAgent` function

## Problem Statement

`logFileForAgent()` in `internal/daemon/jsonl.go:146-156` is defined but never called. `BuildAgentDetail` does its own agent lookup and calls `logFilePath()` directly. Dead code costs mental energy every time someone reads the file.

## Findings

- Found by: code-reviewer, simplicity-reviewer, grug-brain, architecture-strategist, pattern-recognition
- All 5 agents independently flagged this as dead code
- `BuildAgentDetail` does its own pool lookup (status.go:151-161) and calls `logFilePath()` directly
- The function also duplicates agent-lookup logic

## Proposed Solutions

### Option 1: Delete the function

**Approach:** Remove `logFileForAgent` entirely. 12 lines deleted.

**Pros:** Eliminates confusion, reduces surface area
**Cons:** None
**Effort:** 1 minute
**Risk:** None — function has zero callers

## Acceptance Criteria

- [ ] `logFileForAgent` removed from `jsonl.go`
- [ ] `go build ./...` succeeds
- [ ] `go test ./... -race` passes

## Work Log

### 2026-02-07 - Discovery

**By:** Multi-agent code review (7 agents)
**Actions:** Identified dead function via grep — zero callers in codebase
