---
status: pending
priority: p3
issue_id: "011"
tags: [code-review, quality]
dependencies: []
---

# Collapse `extractKeyInput` switch cases

## Problem Statement

In `internal/daemon/jsonl.go:107-121`, three cases (`read`, `edit`, `write`) all return `unquoteField(m, "filePath")` and two cases (`glob`, `grep`) both return `unquoteField(m, "pattern")`. Standard Go idiom is to collapse identical cases.

## Findings

- Found by: code-reviewer, simplicity-reviewer
- 7 case arms can be reduced to 4 using Go's multi-value case syntax

## Proposed Solutions

### Option 1: Collapse cases

```go
case "read", "edit", "write":
    return unquoteField(m, "filePath")
case "bash":
    return unquoteField(m, "command")
case "glob", "grep":
    return unquoteField(m, "pattern")
case "task":
    return unquoteField(m, "description")
```

**Effort:** 2 minutes
**Risk:** None

## Acceptance Criteria

- [ ] Switch cases collapsed
- [ ] Tests pass

## Work Log

### 2026-02-07 - Discovery

**By:** Multi-agent code review
