---
title: proposal: Review local spawn-policy changes
type: proposal
date: 2026-02-16
---

# proposal: Review local spawn-policy changes

## Overview

Review the current uncommitted branch changes that introduce the daemon `spawn_policy` behavior (`auto` vs `manual`) and verify correctness across config validation, daemon runtime behavior, status rendering, and operator-facing CLI/TUI output.

## Problem Statement / Motivation

The local diff modifies daemon orchestration behavior and makes `prog` optional in manual mode. This is a correctness-sensitive change because it affects:

- startup validation (`project` required only in `auto`)
- runtime loops (polling/reconcile enabled or disabled by policy)
- status data shape and enrichment (manual path skips `prog` calls)
- user visibility of mode in CLI/TUI output

Without a focused review plan, regressions can slip into startup behavior, status correctness, or operator expectations.

## Proposed Solution

Execute a targeted, file-scoped review with policy-specific scenarios:

- `auto` policy baseline behavior remains intact
- `manual` policy behaves spawn-only and does not depend on `prog`
- CLI/TUI make policy visible and avoid misleading queue semantics
- tests capture both config and runtime policy branching

## Technical Considerations

- Config validation changed to conditional project requirement in `internal/daemon/config.go`.
- Daemon loop startup now branches on policy in `internal/daemon/daemon.go`.
- Full status now includes policy and manual fast-path behavior in `internal/daemon/status.go`.
- Client + command/UI surfaces expose policy in `internal/client/client.go`, `cmd/af/cmd/status.go`, `internal/tui/tui.go`, and `cmd/af/cmd/daemon.go`.
- Existing TODOs indicate known review follow-ups:
  - `todos/030-pending-p2-manual-spawn-policy-still-requires-prog-project.md`
  - `todos/031-pending-p2-add-daemon-manual-policy-behavior-tests.md`

## Review Scope

In scope:

- `README.md`
- `cmd/af/cmd/daemon.go`
- `cmd/af/cmd/status.go`
- `internal/client/client.go`
- `internal/daemon/config.go`
- `internal/daemon/config_test.go`
- `internal/daemon/daemon.go`
- `internal/daemon/status.go`
- `internal/daemon/status_test.go`
- `internal/tui/tui.go`

Out of scope:

- unrelated task scheduling behavior not touched by this diff
- production deployment changes

## Acceptance Criteria

- [ ] `spawn_policy` accepts only `auto|manual`, defaults correctly, and rejects invalid values.
- [ ] `project` is required when `spawn_policy=auto` and optional when `spawn_policy=manual`.
- [ ] Daemon startup disables poll/reconcile auto-scheduling loops in manual mode.
- [ ] `status.full` includes `spawn_policy` and skips all `prog` enrichment calls in manual mode.
- [ ] `af status` and TUI visibly indicate non-auto policy (`[spawn:manual]`).
- [ ] Manual-mode status works without a configured project and without `prog` availability.
- [ ] Tests cover config validation and status behavior for both auto and manual paths.
- [ ] README and CLI flag docs align with implemented behavior and constraints.

## Test and Verification Plan

1. Unit tests:
   - `go test ./internal/daemon -run 'TestConfig|TestBuildFullStatus|TestStatus'`
   - Ensure manual-mode tests assert no runner/prog calls.
2. CLI formatting checks:
   - verify status output includes policy annotation path in `cmd/af/cmd/status.go`.
   - verify daemon start messaging includes policy in `cmd/af/cmd/daemon.go`.
3. Documentation consistency check:
   - cross-check README policy docs against actual validation and runtime behavior.
4. Gap closure:
   - resolve or explicitly defer TODO 030 and TODO 031 with rationale.

## Risks & Mitigations

- Risk: Manual mode still reaches `prog` indirectly on some status path.
  - Mitigation: add negative tests that fail on any runner invocation in manual mode.
- Risk: Operators misinterpret empty queue in manual mode as error.
  - Mitigation: keep explicit mode annotation and document queue expectations.
- Risk: Runtime branch disables more than intended in manual mode.
  - Mitigation: validate loop gating in daemon-level tests, not only status tests.

## References & Research

- Internal code references:
  - `internal/daemon/config.go`
  - `internal/daemon/daemon.go`
  - `internal/daemon/status.go`
  - `internal/daemon/status_test.go`
  - `cmd/af/cmd/status.go`
  - `internal/tui/tui.go`
- Related docs:
  - `README.md`
  - `docs/swarm-feedback-loops.md`
- Related local follow-ups:
  - `todos/030-pending-p2-manual-spawn-policy-still-requires-prog-project.md`
  - `todos/031-pending-p2-add-daemon-manual-policy-behavior-tests.md`
