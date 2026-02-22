---
status: pending
priority: p2
issue_id: "076"
tags: [code-review, architecture]
dependencies: []
---

# Extract spawn orchestration from CLI to daemon package

## Problem Statement

`cmd/af/cmd/spawn.go:157-283` — `runSpritesSpawn()` is 130 lines of orchestration (store ops, API calls, state transitions, error classification) in the CLI layer. This violates the established pattern where CLI is a thin IO shell. When daemon becomes a hosted service, this logic needs to move anyway.

## Findings

- `runSpritesSpawn()` performs store operations, API calls, state transitions, and error classification — all business logic that belongs in the daemon layer.
- The CLI layer should only handle argument parsing, formatting, and printing.
- The established pattern in this codebase is for CLI commands to be thin wrappers around daemon package functions.
- This function will need to move when the daemon becomes a hosted service, so extracting it now avoids future churn.
- The function is 130 lines of tightly coupled orchestration, making it hard to test without invoking the full CLI.

## Proposed Solution

- Extract a `StartRemoteSpawn(ctx context.Context, req SpawnRequest) (SpawnResult, error)` function into the daemon package.
- Move all store ops, API calls, state transitions, and error classification into this function.
- CLI `runSpritesSpawn()` becomes: parse args, call `StartRemoteSpawn`, format and print result.
- The daemon function should return structured results and typed errors that the CLI can format for display.

## Acceptance Criteria

- [ ] `StartRemoteSpawn` (or equivalent) exists in the daemon package with all orchestration logic.
- [ ] `runSpritesSpawn()` in the CLI is reduced to argument parsing, calling the daemon function, and formatting output.
- [ ] No store operations, API calls, or state transitions remain in the CLI layer.
- [ ] Existing behavior is preserved (same outputs, same error messages).
- [ ] The extracted function is independently testable.

## Work Log

- (none yet)
