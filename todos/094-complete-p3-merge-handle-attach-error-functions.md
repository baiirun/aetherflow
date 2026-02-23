---
status: pending
priority: p3
issue_id: "094"
tags: [code-review, quality]
dependencies: []
---

# Merge `handleAttachError` and `handleAttachErrorWithSpawn` into one function

## Problem Statement

Two functions do nearly the same thing — the "WithSpawn" variant just adds `State` and `SpawnID` fields to the JSON output. Since `attachErrorResult` has `omitempty` on those fields, a single function accepting an optional `*RemoteSpawnRecord` would produce the same JSON for both cases.

## Findings

- `cmd/af/cmd/sessions.go:540-560` — two functions, ~20 lines
- `attachErrorResult` already has `omitempty` on `State` and `SpawnID`

## Proposed Solutions

### Option 1: Single function with optional record parameter

**Approach:** One `handleAttachError(jsonOut bool, code string, rec *daemon.RemoteSpawnRecord, err error)` — callers without a spawn record pass `nil`.

**Effort:** 10 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Single function replaces both
- [ ] JSON output unchanged for both call patterns

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified function duplication in attach error handling
