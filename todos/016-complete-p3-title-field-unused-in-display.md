---
status: pending
priority: p3
issue_id: "016"
tags: [code-review, quality]
dependencies: []
---

# Verify `ToolCall.Title` field is useful or remove it

## Problem Statement

`ToolCall.Title` is populated from `line.Part.Title` but never displayed in `printAgentDetail` — the CLI only shows `tc.Tool` and `tc.Input`. If the field only exists for JSON output consumers, that's fine. If nobody uses it, it's dead weight on the wire.

## Findings

- Found by: simplicity-reviewer
- `Title` populated at jsonl.go:69
- Never referenced in `printAgentDetail` (status.go CLI formatter)
- Available in `--json` output for programmatic consumers

## Proposed Solutions

### Option 1: Display it in CLI when non-empty

**Approach:** Show title as a hint after the tool name when available:
```go
fmt.Printf("  %6s  %-10s %s  %s%s\n", relTime, tc.Tool, tc.Title, input, dur)
```

**Effort:** 5 minutes
**Risk:** None

### Option 2: Remove it

**Approach:** Delete from both daemon and client types.
**Effort:** 5 minutes
**Risk:** None

## Acceptance Criteria

- [ ] Decision made: display or remove
- [ ] Implementation matches decision

## Work Log

### 2026-02-07 - Discovery

**By:** Multi-agent code review (simplicity-reviewer)
