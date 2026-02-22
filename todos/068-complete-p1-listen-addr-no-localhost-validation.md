---
status: pending
priority: p1
issue_id: "068"
tags: [code-review, security]
dependencies: []
---

# ListenAddr is YAML-configurable with no localhost validation

## Problem Statement

`internal/daemon/config.go:85` exposes `ListenAddr` as `yaml:"listen_addr"`. Previously `SocketPath` was `yaml:"-"` (not configurable). A user can set `listen_addr: "0.0.0.0:7070"` and expose the unauthenticated daemon API to the network. Combined with no auth, this is a remote takeover vector.

## Findings

- Security-sentinel, code-reviewer, and architecture-strategist all flagged this independently.
- The `ListenAddr` field is user-configurable via YAML without any validation on the bind address.
- The daemon API has no authentication, so binding to a non-loopback address exposes full control of the daemon to anyone on the network.
- This is a critical escalation path: network-accessible + unauthenticated = remote takeover.

## Proposed Solution

1. Add validation in `Config.Validate()` that rejects non-loopback bind addresses.
2. Only `127.0.0.1` and `localhost` should be allowed as bind hosts until authentication is implemented.
3. Reject addresses like `0.0.0.0`, `:7070` (empty host), and any explicitly non-loopback host.

**Effort:** Small (15 min)

## Acceptance Criteria

- [ ] `Config.Validate()` rejects `0.0.0.0`, `:7070`, and any non-loopback host in `ListenAddr`.
- [ ] `Config.Validate()` accepts `127.0.0.1` and `localhost` as valid bind hosts.
- [ ] Test cases cover all rejection and acceptance scenarios.

## Work Log
