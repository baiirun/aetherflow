---
status: pending
priority: p3
issue_id: "120"
tags: [code-review, correctness, unicode]
dependencies: []
---

# truncateLastError should use rune-safe truncation

## Problem Statement

`truncateLastError` uses byte-length truncation (`len(s)` and `s[:maxLastErrorLen]`), which can split multi-byte UTF-8 sequences. `truncatePrompt` in the same file already handles this correctly with `[]rune` conversion. While provider errors are typically ASCII, a non-ASCII error would produce invalid UTF-8 on the wire.

## Findings

- `internal/daemon/status.go:383-388` — byte-based truncation
- `internal/daemon/status.go:391-397` — `truncatePrompt` uses `[]rune` correctly
- Go's `json.Marshal` replaces invalid UTF-8 with `\ufffd` — not a crash, but garbled output
- Flagged by: code-reviewer, security-sentinel, performance-oracle, data-integrity-guardian, tigerstyle-reviewer (5/10 agents)

## Proposed Solutions

### Option 1: Switch to rune-based truncation

**Approach:** Use `[]rune` conversion like `truncatePrompt`. Rename constant to `maxLastErrorBytes` for clarity.

**Effort:** 5 minutes
**Risk:** Low

### Option 2: Keep byte-based, document the assumption

**Approach:** Rename to `maxLastErrorBytes`, add comment documenting the ASCII assumption.

**Effort:** 2 minutes
**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status.go:343-388` — constant and function

## Acceptance Criteria

- [ ] `truncateLastError` cannot produce invalid UTF-8
- [ ] Constant name clarifies bytes vs runes
- [ ] `TestTruncateLastError` updated if needed

## Work Log

### 2026-02-22 - Code Review Round 5

**By:** Claude Code

**Actions:**
- 5 of 10 agents flagged byte-vs-rune inconsistency
