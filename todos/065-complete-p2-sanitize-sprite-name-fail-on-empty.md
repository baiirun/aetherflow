---
status: complete
priority: p2
issue_id: "065"
tags: [code-review, correctness, input-validation]
dependencies: []
---

# sanitizeSpriteName("") should fail fast, not return "sprite"

## Problem Statement

`sanitizeSpriteName("")` returns `"sprite"` as a fallback, which silently creates a sprite with a generic name instead of surfacing the programming error that led to an empty name being passed.

## Recommended Action

Return an error for empty input. Callers should validate before calling, and if they don't, the error surfaces the bug immediately rather than creating phantom sprites.

## Technical Details

**Affected files:**
- `internal/daemon/sprites_client.go` — `sanitizeSpriteName()`
- All callers: `Create()`, `GetStatus()`, `Terminate()`

## Acceptance Criteria

- [ ] `sanitizeSpriteName` returns an error for empty input
- [ ] All callers handle the error
- [ ] Test covers empty input case
