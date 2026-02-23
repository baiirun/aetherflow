---
status: pending
priority: p3
issue_id: "096"
tags: [code-review, quality]
dependencies: []
---

# Consolidate `truncateString` (byte-based) with `truncate` (rune-based)

## Problem Statement

Two truncation functions exist in the same package with subtly different behavior:
- `truncateString` in `sessions.go:603` — byte-based, ASCII ellipsis (`...`)
- `truncate` in `status.go` — rune-based, Unicode ellipsis (`…`)

The byte-based version could split a multi-byte UTF-8 character at the boundary.

## Findings

- `sessions.go:603` — `truncateString(s string, max int)` uses `len(s)` (bytes)
- `status.go` — `truncate(s string, max int)` uses `[]rune` (rune-aware)
- The rune-based version is more correct for display

## Proposed Solutions

### Option 1: Consolidate on rune-aware version

**Approach:** Replace `truncateString` calls with `truncate` from `status.go`, or make `truncateString` rune-aware.

**Effort:** 15 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Single truncation function in the package
- [ ] Handles multi-byte characters correctly

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified duplicate truncation functions with inconsistent behavior
