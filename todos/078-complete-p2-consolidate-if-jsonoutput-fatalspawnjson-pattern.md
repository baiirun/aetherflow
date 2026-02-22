---
status: pending
priority: p2
issue_id: "078"
tags: [code-review, simplicity]
dependencies: []
---

# Consolidate repeated if-jsonOutput-fatalSpawnJSON pattern

## Problem Statement

`cmd/af/cmd/spawn.go:159-244` — The `if jsonOutput { fatalSpawnJSON(...) } Fatal(...)` pattern appears 8 times in `runSpritesSpawn`, producing ~50 lines of branching logic.

## Findings

- The same conditional pattern is repeated 8 times across the function.
- Each occurrence checks `jsonOutput`, calls `fatalSpawnJSON` with structured error data if true, and calls `Fatal` with a formatted message if false.
- The repetition adds visual noise and increases the chance of inconsistency (e.g., different error codes or missing fields in one branch).
- This is a straightforward extract-method refactoring opportunity.

## Proposed Solution

- Extract a `fatalSpawn(jsonOutput bool, code, spawnID string, format string, args ...any)` helper function.
- The helper encapsulates the conditional: if `jsonOutput`, call `fatalSpawnJSON`; otherwise, call `Fatal`.
- Replace all 8 occurrences with calls to the helper.

## Acceptance Criteria

- [ ] A `fatalSpawn` helper (or equivalent) encapsulates the json/text branching logic.
- [ ] All 8 occurrences of the pattern are replaced with calls to the helper.
- [ ] ~50 lines of branching are reduced to ~8 single-line calls.
- [ ] Error codes, spawn IDs, and messages remain identical to current behavior.
- [ ] Existing tests pass without modification.

## Work Log

- (none yet)
