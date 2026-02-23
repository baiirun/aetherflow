---
status: pending
priority: p3
issue_id: "125"
tags: [code-review, testing, data-integrity]
dependencies: []
---

# Wire contract test should verify omitempty behavior

## Problem Statement

`TestRemoteSpawnStatusWireContract` verifies required keys are present and forbidden keys are absent, but doesn't verify that `omitempty` fields are actually omitted when empty. If someone accidentally removes an `omitempty` tag, the API response would include `"last_error": ""` — a wire contract change that could break clients doing presence checks.

## Findings

- `internal/daemon/status_test.go:446-504` — test sets non-empty values for all optional fields
- `omitempty` fields: `provider_sandbox_id`, `server_ref`, `session_id`, `last_error`
- Need a second marshal of a zero-valued struct to test omission
- Flagged by: data-integrity-guardian

## Proposed Solutions

### Option 1: Add omitempty assertion to existing test

**Approach:** Marshal a minimal `RemoteSpawnStatus` (only required fields set) and assert that omitempty fields are absent from the JSON.

**Effort:** 10 minutes
**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status_test.go` — `TestRemoteSpawnStatusWireContract`

## Acceptance Criteria

- [ ] Test marshals a zero-valued optional struct and verifies omitted keys
- [ ] Test fails if an `omitempty` tag is accidentally removed

## Work Log

### 2026-02-22 - Code Review Round 5

**By:** Claude Code

**Actions:**
- Data integrity guardian identified test coverage gap
