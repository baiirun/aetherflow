---
status: pending
priority: p3
issue_id: "095"
tags: [code-review, testing]
dependencies: []
---

# Slim JSON contract tests — remove flat-struct tests, keep embedding test

## Problem Statement

Three JSON contract tests (96 lines) test that Go's `encoding/json` correctly serializes flat structs. The `attachPendingResult` and `attachErrorResult` tests add zero safety beyond what the struct tags already guarantee. Only the `sessionListEntry` test is valuable — it verifies the embedding flattens correctly.

## Findings

- `TestAttachPendingResultJSONContract` — tests a flat struct with explicit tags (trivially correct)
- `TestAttachErrorResultJSONContract` — same
- `TestSessionListEntryJSONContract` — tests embedding flattens (actually valuable)

## Proposed Solutions

### Option 1: Remove flat-struct tests, slim embedding test

**Approach:** Delete `TestAttachPendingResultJSONContract` and `TestAttachErrorResultJSONContract`. Slim `TestSessionListEntryJSONContract` to ~10 lines focusing on embedding flattening.

**Effort:** 10 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] Embedding test retained and covers flattening
- [ ] Flat-struct tests removed
- [ ] ~70 lines of test code saved

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified over-testing of JSON serialization for flat structs
