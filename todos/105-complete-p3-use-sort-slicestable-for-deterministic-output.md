---
status: pending
priority: p3
issue_id: "105"
tags: [code-review, quality, determinism]
dependencies: []
---

# Use sort.SliceStable for deterministic session listing output

## Problem Statement

`buildSessionListEntries` uses `sort.Slice` which is not stable. If two entries have identical timestamps, their relative order is non-deterministic across invocations. This makes test assertions fragile and output unpredictable.

## Findings

- `cmd/af/cmd/sessions.go:228-238` — `sort.Slice` used instead of `sort.SliceStable`
- For a listing command this is cosmetic, but deterministic output is a quality-of-life improvement

## Proposed Solutions

### Option 1: Switch to sort.SliceStable (Recommended)

**Approach:** Replace `sort.Slice` with `sort.SliceStable`. Negligible performance cost at expected scale.

**Effort:** 2 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `sort.SliceStable` used instead of `sort.Slice`
- [ ] `go test ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code
