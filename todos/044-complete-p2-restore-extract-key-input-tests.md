---
status: complete
priority: p2
issue_id: "044"
tags: [code-review, testing, coverage-gap]
dependencies: []
---

# Restore extractKeyInput test coverage

## Problem Statement

Commit f644f5f deleted `jsonl_test.go` which contained `TestExtractKeyInput` — a 12-case table-driven test covering all tool types, unknown tools, empty input, and null input. The function `extractKeyInput()` survived the deletion (used by `event_tools.go` and `logfmt.go`) but lost its direct test coverage.

## Findings

- `TestExtractKeyInput` had 12 test cases: read, edit, write, bash, glob, grep, task, skill, webfetch, unknown-with-filePath, empty, null
- The function is indirectly exercised by `TestToolCallsFromEventsHappyPath` and `TestFormatEvent_Tool` but those don't cover edge cases (empty, null, unknown tools, fallback field probing)
- The `default` case in `extractKeyInput` tries 7 common field names — this logic path has zero test coverage now
- Flagged by code-reviewer and git-history-analyzer agents

## Proposed Solutions

### Option 1: Port test to event_tools_test.go (Recommended)

**Approach:** Copy the table-driven `TestExtractKeyInput` test into `event_tools_test.go` where the primary consumer lives.

**Pros:** Restores full coverage, co-locates test with consumer
**Cons:** None
**Effort:** 10 minutes
**Risk:** Low

## Recommended Action



## Acceptance Criteria

- [ ] `TestExtractKeyInput` exists with all 12 original test cases
- [ ] Tests pass
- [ ] Coverage of `extractKeyInput` includes the `default` case fallback

## Work Log

### 2026-02-19 - Code Review Discovery

**By:** Claude Code (git-history-analyzer agent)

**Actions:**
- Identified test coverage gap from deleted jsonl_test.go
- Verified no other test covers extractKeyInput edge cases
- Original test had: read, edit, write, bash, glob, grep, task, skill, webfetch, unknown, empty, null cases
