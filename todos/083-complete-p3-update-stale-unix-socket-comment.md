---
status: pending
priority: p3
issue_id: "083"
tags: [code-review, documentation]
dependencies: []
---

# Update stale Unix socket comment in event_rpc.go

## Problem Statement

`internal/daemon/event_rpc.go:19` still says "These arrive from the opencode plugin via the daemon's Unix socket." Transport is now HTTP.

## Findings

- The comment references the old Unix socket transport mechanism which has been replaced by HTTP.
- This is a stale comment left over from the transport migration.
- Misleading comments erode trust in documentation across the codebase.

## Proposed Solution

Update comment to reference HTTP API.

## Acceptance Criteria

- [ ] The comment at `internal/daemon/event_rpc.go:19` accurately references HTTP transport instead of Unix socket.

## Work Log

- **Effort estimate:** Small (2 min)
