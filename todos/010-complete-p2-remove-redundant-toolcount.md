---
status: pending
priority: p2
issue_id: "010"
tags: [code-review, quality, api-design]
dependencies: []
---

# Remove redundant `ToolCount` field from `AgentDetail`

## Problem Statement

`AgentDetail.ToolCount` is always `len(detail.ToolCalls)` (status.go:222). The comment says "for an accurate count we'd need to scan the whole file" but doesn't do that. The field implies it might differ from the slice length, but it never does — misleading for consumers.

## Findings

- Found by: code-reviewer, simplicity-reviewer, grug-brain, architecture-strategist
- `ToolCount` set at status.go:222 as `detail.ToolCount = len(detail.ToolCalls)`
- The field exists in both `daemon.AgentDetail` and `client.AgentDetail`
- The daemon.go log line uses `detail.ToolCount` but could use `len(detail.ToolCalls)`
- The CLI already uses `len(d.ToolCalls)` directly

## Proposed Solutions

### Option 1: Remove the field now, add back when meaningful

**Approach:** Delete `ToolCount` from both packages. Use `len(ToolCalls)` at call sites.

**Pros:** Eliminates misleading wire protocol field, reduces surface area
**Cons:** If we later need total-vs-returned count, we add it then
**Effort:** 10 minutes (3 files)
**Risk:** None — field has no independent meaning

## Acceptance Criteria

- [ ] `ToolCount` removed from `daemon.AgentDetail` and `client.AgentDetail`
- [ ] `daemon.go` log line uses `len(detail.ToolCalls)` directly
- [ ] `go test ./... -race` passes

## Work Log

### 2026-02-07 - Discovery

**By:** Multi-agent code review
**Actions:** Identified redundant field — always equals `len(ToolCalls)`
