---
status: complete
priority: p3
issue_id: "049"
tags: [code-review, performance, simplification]
dependencies: []
---

# Replace bubble sort in ToolCallsFromEvents with sort.Slice

## Problem Statement

`ToolCallsFromEvents` in `event_tools.go` (lines 92-98) uses a hand-rolled O(n²) bubble sort to order tool call entries by insertion order. The stdlib `sort.Slice` or `slices.SortFunc` would be clearer and O(n log n).

## Findings

- The sort operates on unique tool call entries (typically 5-50 items per session)
- At this scale, O(n²) vs O(n log n) has negligible performance difference
- The real issue is readability — 7 lines of nested loops vs 3 lines of stdlib call
- Flagged by simplicity-reviewer, tigerstyle-reviewer, and performance-oracle

## Proposed Solutions

### Option 1: Use sort.Slice (Recommended)

**Approach:** Replace nested for loops with `sort.Slice(ordered, func(i, j int) bool { return ordered[i].order < ordered[j].order })`

**Pros:** Idiomatic, readable, O(n log n)
**Cons:** None
**Effort:** 5 minutes
**Risk:** Low

## Recommended Action



## Acceptance Criteria

- [ ] Bubble sort replaced with stdlib sort
- [ ] All ToolCallsFromEvents tests pass
- [ ] `go build ./...` passes

## Work Log

### 2026-02-19 - Code Review Discovery

**By:** Claude Code (simplicity-reviewer + tigerstyle-reviewer)
