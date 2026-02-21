---
status: complete
priority: p2
issue_id: "061"
tags: [code-review, tests, reliability]
dependencies: []
---

# Add regression tests for attach-by-spawn behavior

## Problem Statement

New attach-by-spawn path lacks dedicated tests, leaving high-risk edge cases unguarded.

## Findings

- `cmd/af/cmd/sessions_test.go` currently focuses on helper functions.
- No test coverage for remote spawn store attach path (`pending`, `failed`, `server filter`, JSON output, exit codes).

## Proposed Solutions

### Option 1: Table-driven command-level tests

**Approach:** Add table tests around `runSessionAttach` with fake stores and output capture.

**Pros:** Broad coverage quickly.

**Cons:** Requires some command wiring seams.

**Effort:** 3-5 hours

**Risk:** Low

## Recommended Action

Implement Option 1 with core edge cases first.

## Technical Details

**Affected files:**
- `cmd/af/cmd/sessions_test.go`

## Acceptance Criteria

- [ ] tests cover pending vs terminal spawn states
- [ ] tests cover `--server` filter behavior in spawn fallback path
- [ ] tests cover JSON output contract and exit code semantics

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Captured explicit testing gap from code-review findings.
