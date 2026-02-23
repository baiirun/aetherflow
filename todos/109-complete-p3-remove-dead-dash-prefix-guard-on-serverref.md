---
status: pending
priority: p3
issue_id: "109"
tags: [code-review, dead-code, simplicity]
dependencies: []
---

# Remove dead dash-prefix guard on ServerRef

## Problem Statement

In `runSessionAttach`, the dash-prefix check on `target.ServerRef` (line 515-517) is dead code. `ValidateServerURLAttachTarget` (called on line 512) requires the URL to have an `http` or `https` scheme — any string starting with `-` will fail `url.ParseRequestURI` and be rejected there. This check can never trigger.

## Findings

- `cmd/af/cmd/sessions.go:515-517` — unreachable after `ValidateServerURLAttachTarget` succeeds
- `ValidateServerURLAttachTarget` at `internal/daemon/server_url.go:45` validates scheme, host, and rejects non-URL strings

## Proposed Solutions

### Option 1: Remove the 3 lines (Recommended)

**Effort:** 2 minutes
**Risk:** Low — the validation is already done by `ValidateServerURLAttachTarget`

## Acceptance Criteria

- [ ] Dash-prefix check on ServerRef removed
- [ ] `go build ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified by simplicity-reviewer agent during round 3 review
