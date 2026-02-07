---
status: pending
priority: p2
issue_id: "012"
tags: [code-review, observability, logging]
dependencies: []
---

# Return count of skipped JSONL lines for observability

## Problem Statement

`ParseToolCalls` silently skips malformed JSONL lines (`continue` on parse error). When a log file is corrupted, `af status <agent>` shows 0 tool calls with no indication that lines were skipped. At 3am debugging why agent status shows nothing, there's zero signal that the parser is silently dropping lines.

## Findings

- Found by: grug-brain (primary), architecture-strategist
- Malformed lines skipped silently at jsonl.go:60-61
- No log, no counter, no indication to caller
- Real scenario: agent crash writes partial line, entire tail of file may fail to parse

## Proposed Solutions

### Option 1: Return skipped count alongside results

**Approach:** Change `ParseToolCalls` return to `([]ToolCall, int, error)` where the int is skipped line count. Caller logs when `skipped > 0`.

**Pros:** Minimal change, provides signal at the right layer
**Cons:** Changes function signature (but only 2 callers)
**Effort:** 15 minutes
**Risk:** Low

### Option 2: Log inside ParseToolCalls

**Approach:** Pass a logger and log once at end of scan with skipped count.

**Pros:** Self-contained
**Cons:** Adds logger dependency to a pure parser function
**Effort:** 10 minutes
**Risk:** Low

## Recommended Action

Option 1 — keep the parser pure, let the caller decide what to do with the count.

## Acceptance Criteria

- [ ] `ParseToolCalls` returns skipped line count
- [ ] `BuildAgentDetail` logs when skipped > 0
- [ ] Tests updated for new return value
- [ ] Test for malformed lines verifies skipped count

## Work Log

### 2026-02-07 - Discovery

**By:** Multi-agent code review (grug-brain)
**Key quote:** "grug wake up at 3am, agent status show 0 tool calls but agent clearly running. WHY?"
