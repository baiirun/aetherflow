---
status: pending
priority: p3
issue_id: "081"
tags: [code-review, naming]
dependencies: []
---

# Rename protocol/socket.go to protocol/daemon_url.go

## Problem Statement

`internal/protocol/socket.go` contains `DaemonURLFor`, `DefaultDaemonURL`, `DefaultDaemonPort` — nothing socket-related anymore. Misleading filename.

## Findings

- The file was originally named for Unix socket functionality, but after the HTTP transport migration it now contains URL and port helpers.
- All exported symbols (`DaemonURLFor`, `DefaultDaemonURL`, `DefaultDaemonPort`) relate to daemon URLs, not sockets.
- The filename creates a false expectation about the file's contents.

## Proposed Solution

Rename to `daemon_url.go` (and test file to `daemon_url_test.go`).

## Acceptance Criteria

- [ ] `internal/protocol/socket.go` is renamed to `internal/protocol/daemon_url.go`.
- [ ] `internal/protocol/socket_test.go` is renamed to `internal/protocol/daemon_url_test.go`.
- [ ] All imports and references still compile and tests pass.

## Work Log

- **Effort estimate:** Small (5 min)
