---
status: complete
priority: p2
issue_id: "063"
tags: [code-review, api-contract, json]
dependencies: []
---

# af spawn --json error paths should emit structured JSON

## Problem Statement

When `af spawn --json` encounters errors (e.g., missing SPRITES_TOKEN, store failures), it calls `Fatal()` which writes plain text to stderr. This breaks the machine-readable contract — callers parsing JSON get nothing on stdout and unstructured text on stderr.

## Findings

- `runSpritesSpawn()` uses `Fatal()` for errors even when `jsonOutput` is true
- The success path correctly emits JSON but all error paths bypass it
- Agents/scripts consuming `--json` output can't distinguish error types programmatically

## Recommended Action

For all error exits in `runSpritesSpawn()` when `jsonOutput` is true, emit a structured JSON error object to stdout before exiting.

## Technical Details

**Affected files:**
- `cmd/af/cmd/spawn.go` — `runSpritesSpawn()` error paths

## Acceptance Criteria

- [ ] All error paths in `runSpritesSpawn` emit JSON when `--json` flag is set
- [ ] JSON error objects include `success: false`, `code`, and `error` fields
- [ ] Exit codes are non-zero for errors
