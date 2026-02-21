---
status: complete
priority: p2
issue_id: "055"
tags: [code-review, reliability, observability]
dependencies: []
---

# Surface remote spawn store errors instead of masking as not found

## Problem Statement

Attach flow currently swallows remote spawn store open/read errors and may present them as “not found,” hiding real corruption/permission/schema problems.

## Findings

- `cmd/af/cmd/sessions.go:328` ignores `rsErr`.
- `cmd/af/cmd/sessions.go:330` ignores `getErr`.

## Proposed Solutions

### Option 1: Fail fast on non-not-found errors

**Approach:** Return explicit CLI/JSON errors for store open/read failures.

**Pros:** Better diagnostics and safer operations.

**Cons:** Slightly noisier UX for transient errors.

**Effort:** 1-2 hours

**Risk:** Low

## Recommended Action

Implement Option 1 and include stable error code.

## Technical Details

**Affected files:**
- `cmd/af/cmd/sessions.go`

## Acceptance Criteria

- [ ] store open/read failures surface clearly in human output
- [ ] `--json` output includes deterministic error code
- [ ] “not found” only used when data source was read successfully

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Recorded hidden-error-path defect from tigerstyle/data-integrity reviews.
