---
status: complete
priority: p2
issue_id: "030"
tags: [code-review, daemon, configuration, status]
dependencies: []
---

# Manual spawn policy still depends on prog project context

## Problem Statement

`spawn_policy: manual` is intended to run the daemon in spawn-only mode, but the current implementation still requires a `project` and still executes `prog` commands in status paths. That keeps a hard runtime dependency on `prog` even when auto-scheduling is disabled.

## Findings

- Config validation always requires `project` regardless of policy (`internal/daemon/config.go:138`).
- Full status always fetches queue via `prog ready` (`internal/daemon/status.go:126`) even in manual mode.
- Runtime loop correctly disables poll/reclaim/reconcile in manual mode (`internal/daemon/daemon.go:121-153`), so behavior is partially manual-only but config/status semantics are inconsistent.

## Proposed Solutions

### Option 1: Make manual mode fully prog-optional (recommended)

- If `spawn_policy == manual`, allow empty `project` in validation.
- In `BuildFullStatus`, skip queue/prog enrichment when policy is manual.
- Show an explicit status note like `queue: disabled in manual mode`.
- **Effort:** Medium
- **Risk:** Medium

### Option 2: Keep `project` required, but skip prog calls in manual mode

- Preserve current startup contract.
- Gate `fetchQueue` and `fetchTaskSummary` by policy.
- **Effort:** Small
- **Risk:** Low

### Option 3: Document current semantics as intentional

- Clarify that manual mode disables auto-spawn only, not prog coupling.
- **Effort:** Small
- **Risk:** Medium (user expectation mismatch remains)

## Recommended Action

Completed via manual-mode hardening and policy simplification.

## Technical Details

- Affected files: `internal/daemon/config.go`, `internal/daemon/status.go`, `cmd/af/cmd/status.go`, `README.md`
- Components: daemon config validation, status RPC behavior

## Acceptance Criteria

- [x] Manual mode behavior is explicitly defined as spawn-only and prog-optional.
- [x] Implementation matches the contract in startup and status paths.
- [x] Tests cover manual-mode status path without unintended `prog` calls.
- [x] CLI/help text reflects final behavior.

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from workflow review | Manual mode currently disables scheduling loops but still depends on project/prog in config/status layers. |
| 2026-02-16 | Closed after follow-up implementation | `project` is now required only for auto mode; manual status path skips prog enrichment/queue calls; docs and tests align with spawn-only manual semantics. |

## Resources

- PR/branch context: local working tree changes on `main`
