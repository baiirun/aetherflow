---
status: complete
priority: p3
issue_id: "033"
tags: [code-review, cli, ux, documentation]
dependencies: []
---

# Daemon status fallback hint still implies project is always required

## Problem Statement

When `af daemon` cannot connect, it prints a start hint that hardcodes `--project <name>`, even though manual mode now allows starting without project. This creates stale operator guidance.

## Findings

- Fallback message is `To start: af daemon start --project <name>` (`cmd/af/cmd/daemon.go:23-24`).
- New policy semantics permit manual spawn-only mode with no project (`internal/daemon/config.go:156-159` and README updates).

## Proposed Solutions

### Option 1: Update hint to include policy-aware examples (recommended)

- Example:
  - `af daemon start --project <name>` (auto)
  - `af daemon start --spawn-policy manual` (manual)
- **Effort:** Small
- **Risk:** Low

### Option 2: Generic hint without flags

- `To start: af daemon start (run --help for options)`.
- **Effort:** Small
- **Risk:** Low

## Recommended Action

Option 1 for immediate discoverability of the new mode.

## Technical Details

- Affected files: `cmd/af/cmd/daemon.go`
- Components: CLI operator guidance

## Acceptance Criteria

- [x] Non-running daemon hint does not contradict manual mode behavior.
- [x] Help text and fallback text are consistent with spawn policy semantics.

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from workflow review | Operator hint text still reflects pre-manual-policy assumptions. |
| 2026-02-16 | Updated non-running daemon hint with auto/manual examples | Startup guidance now matches spawn policy behavior. |

## Resources

- Review context: local working tree on `main`
