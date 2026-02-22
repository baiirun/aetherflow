---
status: pending
priority: p3
issue_id: "084"
tags: [code-review, security]
dependencies: []
---

# SpritesClient response body decoded without size limit

## Problem Statement

`internal/daemon/sprites_client.go:76` — Success path decodes response without `io.LimitReader`. Error paths correctly use `io.LimitReader(resp.Body, 8192)` but success path does not. A compromised Sprites API could send an unbounded response.

## Findings

- Error response paths correctly wrap `resp.Body` with `io.LimitReader(resp.Body, 8192)`.
- The success path at line 76 decodes directly from `resp.Body` with no size limit.
- This is inconsistent within the same function and leaves the success path vulnerable to memory exhaustion from an unbounded response.
- A compromised or misbehaving Sprites API could exploit this to cause OOM.

## Proposed Solution

Wrap success path with `io.LimitReader(resp.Body, 1<<20)`.

## Acceptance Criteria

- [ ] The success path response body decoding in `internal/daemon/sprites_client.go` is wrapped with `io.LimitReader`.
- [ ] The limit is set to a reasonable maximum (e.g., `1<<20` / 1 MiB).
- [ ] Error paths remain unchanged.
- [ ] Tests pass.

## Work Log

- **Effort estimate:** Small (5 min)
