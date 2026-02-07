---
status: pending
priority: p2
issue_id: "013"
tags: [code-review, security, terminal-injection]
dependencies: []
---

# Harden `stripANSI` to cover `\r`, `\x08`, and other C0 control characters

## Problem Statement

The `stripANSI` regex handles CSI, OSC, and charset sequences, but misses dangerous control characters that a compromised agent could inject via JSONL content: carriage return (`\r`), backspace (`\x08`), delete (`\x7f`), and other C0 controls. A `\r` in tool call input can overwrite the beginning of a terminal line, making the user see fabricated output.

## Findings

- Found by: security-sentinel (primary)
- Current regex: `\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\(B`
- Missing: `\r` (carriage return), `\x08` (backspace), `\x7f` (DEL), DCS (`\x1bP`), APC (`\x1b_`), PM (`\x1b^`)
- Attack surface: JSONL tool call inputs from potentially compromised agents
- Severity: Medium — requires compromised agent writing malicious JSONL

## Proposed Solutions

### Option 1: Strip all C0 control characters + broader escape sequences

**Approach:** Replace `stripANSI` with `stripUnsafe` that removes all C0 controls except tab/newline, plus all ESC-initiated sequences:

```go
var unsafeChars = regexp.MustCompile(`\x1b[^\x1b]*[\x40-\x7e]|\x1b.|\r|\x08|\x7f|[\x00-\x08\x0b\x0c\x0e-\x1f]`)
```

**Pros:** Comprehensive, defense in depth
**Cons:** Might strip legitimate tab characters (unlikely in tool inputs)
**Effort:** 15 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `\r`, `\x08`, `\x7f` stripped from displayed output
- [ ] Existing `stripANSI` tests still pass
- [ ] New test cases for `\r`, `\x08`, DCS sequences
- [ ] All tool call display paths use the hardened function

## Work Log

### 2026-02-07 - Discovery

**By:** Multi-agent code review (security-sentinel)
