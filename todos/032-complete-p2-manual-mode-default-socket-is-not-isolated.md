---
status: complete
priority: p2
issue_id: "032"
tags: [code-review, daemon, security, configuration]
dependencies: []
---

# Manual mode with empty project falls back to shared default socket

## Problem Statement

`spawn_policy=manual` now allows an empty `project`, but socket defaulting still derives from `SocketPathFor(project)`. With an empty project this resolves to `/tmp/aetherd.sock`, which is shared across all such daemons and weakens project-level isolation expectations.

## Findings

- `Config.ApplyDefaults` sets `SocketPath` from `protocol.SocketPathFor(c.Project)` when unset (`internal/daemon/config.go:105-107`).
- `SocketPathFor("")` returns `DefaultSocketPath` (`internal/protocol/socket.go:19-20`).
- `Validate` allows empty `project` in manual mode (`internal/daemon/config.go:156-159`).
- Result: multiple manual daemons without `project`/`socket_path` can contend for a shared socket namespace.

## Proposed Solutions

### Option 1: Require explicit socket path when manual+no-project (recommended)

- If `spawn_policy=manual` and `project==""`, require `socket_path` to be set.
- Keep existing per-project default behavior unchanged for configured projects.
- **Effort:** Small
- **Risk:** Low

### Option 2: Auto-generate unique manual socket path

- Derive socket path from process metadata (pid/timestamp).
- Avoids hard failure but can reduce discoverability for clients.
- **Effort:** Medium
- **Risk:** Medium

### Option 3: Keep shared default and document as intentional

- No runtime guard.
- Higher accidental cross-daemon interference risk.
- **Effort:** Small
- **Risk:** Medium

## Recommended Action

Option 1 to preserve explicit isolation semantics.

## Technical Details

- Affected files: `internal/daemon/config.go`, `internal/protocol/socket.go`, `README.md`
- Components: config defaulting, socket isolation model

## Acceptance Criteria

- [x] Manual mode with empty project does not silently bind shared default socket unless explicitly configured.
- [x] Validation/error messaging clearly explains required config combination.
- [x] Tests cover manual+empty-project behavior with and without `socket_path`.

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from workflow review | Manual policy relaxation introduced a socket-isolation edge case via default path fallback. |
| 2026-02-16 | Added validation guard and config tests for manual+no-project socket behavior | Manual mode now requires explicit socket when project is empty, preserving isolation expectations. |

## Resources

- Review context: local working tree on `main`
