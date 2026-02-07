---
status: pending
priority: p2
issue_id: "001"
tags: [code-review, quality, unicode]
dependencies: []
---

# truncate() uses byte length instead of rune length

## Problem Statement

`truncate()` in `cmd/af/cmd/status.go` uses `len(s)` (byte count) and `s[:max-1]` (byte slice) to truncate strings. If a task title or log message contains multi-byte UTF-8 characters (emoji, CJK, accented letters), this can split a rune mid-byte, producing invalid UTF-8 output in the terminal. The ellipsis `\u2026` is also 3 bytes, so truncated output can exceed `max` bytes.

## Findings

- `len(s)` counts bytes, not characters
- `s[:max-1]` slices by bytes, can split multi-byte runes
- Task titles come from `prog` and could contain any UTF-8 text
- Identified by: code-reviewer, grug-brain-reviewer, pattern-recognition-specialist, simplicity-reviewer

**Affected file:** `cmd/af/cmd/status.go:133-138`

## Proposed Solutions

### Option 1: Convert to runes

**Approach:** Use `[]rune(s)` for length check and slicing.

```go
func truncate(s string, max int) string {
    runes := []rune(s)
    if len(runes) <= max {
        return s
    }
    return string(runes[:max-1]) + "\u2026"
}
```

**Pros:**
- Correct for all UTF-8 strings
- Simple, idiomatic Go

**Cons:**
- Allocates a rune slice (negligible for short strings)

**Effort:** 15 minutes
**Risk:** Low

### Option 2: Keep byte truncation, add comment

**Approach:** Document the limitation.

**Pros:** No code change
**Cons:** Still produces invalid output on multi-byte input

**Effort:** 5 minutes
**Risk:** Low

## Recommended Action

Option 1. Update the test to include a multi-byte string case.

## Acceptance Criteria

- [ ] `truncate()` uses rune-based length and slicing
- [ ] Test added with multi-byte UTF-8 string (e.g., emoji)
- [ ] Existing tests still pass

## Work Log

### 2026-02-07 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified byte-vs-rune issue during code review
- Confirmed by 4 independent review agents
