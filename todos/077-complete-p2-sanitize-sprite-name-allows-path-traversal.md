---
status: pending
priority: p2
issue_id: "077"
tags: [code-review, security]
dependencies: []
---

# sanitizeSpriteName allows path traversal characters

## Problem Statement

`internal/daemon/sprites_client.go:177-185` — `sanitizeSpriteName` only replaces `_` and spaces but does not reject `/`, `..`, `%`, `?`, `#`. The sanitized name is used to construct API paths via `s.baseURL+"/v1/sprites/"+name`. A crafted name could manipulate the Sprites API URL.

## Findings

- `sanitizeSpriteName` performs minimal sanitization: it replaces underscores and spaces but does not filter or reject dangerous characters.
- Characters like `/`, `..`, `%`, `?`, and `#` pass through the sanitizer unchanged.
- The sanitized name is concatenated directly into URL paths for API calls.
- A crafted sprite name containing path traversal sequences (`../`) or query injection characters (`?`, `#`) could manipulate the target URL.
- This is a defense-in-depth issue: even if the Sprites API server validates paths, the client should not send malformed URLs.

## Proposed Solution

- Add an allowlist regex `^[a-z0-9][a-z0-9-]*$` to validate sprite names.
- Reject names containing any characters outside the allowlist with a clear error message.
- Apply validation before URL construction.

## Acceptance Criteria

- [ ] `sanitizeSpriteName` rejects names containing `/`, `..`, `%`, `?`, `#`, and other non-alphanumeric/hyphen characters.
- [ ] An allowlist regex (e.g., `^[a-z0-9][a-z0-9-]*$`) is enforced.
- [ ] Invalid names produce a clear, actionable error message.
- [ ] Unit tests cover rejection of path traversal sequences, query injection characters, and empty names.
- [ ] Existing valid names continue to pass validation.

## Work Log

- (none yet)
