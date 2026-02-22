---
status: pending
priority: p2
issue_id: "071"
tags: [code-review, security]
dependencies: []
---

# Client path/query parameters not URL-encoded (injection risk)

## Problem Statement

`internal/client/client.go:201,238,298` — `agentName` and `spawnID` are interpolated into URL paths and query strings via `fmt.Sprintf` without `url.PathEscape()` or `url.QueryEscape()`. A name containing `/`, `?`, `&`, or `=` corrupts routing or injects parameters.

## Findings

- Path segments constructed with raw string interpolation: agent names and spawn IDs are spliced directly into URL paths.
- Query parameters constructed the same way: values are not escaped, allowing injection of additional query parameters.
- An attacker or misconfigured client could craft names that manipulate routing or inject unintended parameters into API requests.

## Proposed Solution

- Use `url.PathEscape()` for all values interpolated into URL path segments.
- Use `url.QueryEscape()` for all values interpolated into URL query strings.
- Audit all `fmt.Sprintf` calls in `client.go` that construct URLs and apply the appropriate escaping.

## Acceptance Criteria

- [ ] All path segment interpolations in `client.go` use `url.PathEscape()`.
- [ ] All query parameter interpolations in `client.go` use `url.QueryEscape()`.
- [ ] Unit tests confirm that names containing `/`, `?`, `&`, `=`, and `%` are correctly escaped.
- [ ] No raw string interpolation remains in URL construction paths.

## Work Log

- (none yet)
