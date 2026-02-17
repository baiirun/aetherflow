---
status: complete
priority: p1
issue_id: "042"
tags: [code-review, cli, daemon, reliability, configuration]
dependencies: []
---

# Respect config socket_path in client command socket resolution

## Problem Statement

Client-side commands (`af status`, `af stop`, etc.) derive socket path from `--socket`, `--project`, or config `project`, but ignore config `socket_path`. In manual mode without project this can make control commands fail to reach a running daemon.

## Findings

- `cmd/af/cmd/root.go:58` computes socket path without reading config `socket_path`.
- Config parse in `cmd/af/cmd/root.go:78` only includes `project` field.
- Manual mode guidance requires explicit socket when project is empty, increasing reliance on config `socket_path`.

## Proposed Solutions

### Option 1: Include `socket_path` in `resolveSocketPath` config parsing

**Approach:** Parse both `socket_path` and `project`; priority becomes `--socket` > `--project` > config `socket_path` > config `project` > default.

**Pros:**
- Fixes client/daemon contract mismatch.
- Minimal and targeted change.

**Cons:**
- Requires clear precedence documentation.

**Effort:** Small

**Risk:** Low

---

### Option 2: Reuse daemon config loader in CLI layer

**Approach:** Centralize config parsing in a shared package and consume same normalized config for both daemon and client commands.

**Pros:**
- Single source of truth for precedence semantics.

**Cons:**
- Wider refactor and package coupling decisions.

**Effort:** Medium

**Risk:** Medium

## Recommended Action

Superseded by product decision: remove user-defined socket path support entirely and derive sockets only from project/global default.
## Technical Details

- Affected files: `cmd/af/cmd/root.go`, potentially README CLI config docs
- Components: command socket resolution and daemon control UX
- Database changes: No

## Resources

- Review target: commit `006131715aed57dedad6bda8871350e5401f8816`

## Acceptance Criteria

- [x] User-defined socket path support removed from CLI and config loader.
- [x] Socket resolution now uses project/default only.
- [x] Documentation reflects non-configurable socket paths.

## Work Log

### 2026-02-16 - Initial review finding

**By:** Claude Code

**Actions:**
- Captured architecture-level contract mismatch between daemon startup and client socket discovery.

**Learnings:**
- Manual mode introduced a valid projectless path that current client resolution does not fully support.

### 2026-02-16 - Resolved by removing socket overrides

**By:** Claude Code

**Actions:**
- Removed `--socket` CLI flag support and detached forwarding.
- Stopped loading `socket_path` from config files.
- Updated README to state socket paths are derived and not user-configurable.

**Learnings:**
- Tightening the product surface eliminated precedence ambiguity and this class of control-path mismatch.
