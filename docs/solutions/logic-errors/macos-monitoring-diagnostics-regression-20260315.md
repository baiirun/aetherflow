---
module: macos-control-center
date: 2026-03-15
problem_type: logic_error
component: monitoring
symptoms:
  - "the menu bar could only focus the sessions lane, not the specific session the operator intended to inspect"
  - "a selected session with attachable=false only showed `Attach: Pending`, leaving handoff failures implicit"
  - "the ticket claimed menu bar deep links existed, but current main only exposed generic session focus controls"
root_cause: the monitoring model exposed workload ordering and retained detail state, but not first-class menu bar shortcuts or explicit handoff-unavailable messaging
resolution_type: code_fix
severity: medium
tags:
  - macos
  - control-center
  - monitoring
  - diagnostics
  - menu-bar
  - handoff
---

# macOS Monitoring Diagnostics Regression Coverage

## Problem

`ts-9fb41d` was meant to make daemon lifecycle failures diagnosable in the reduced macOS Control Center. When resumed on current `main`, the core reconnect and retained-detail work was already present, but two required operator paths were still missing:

1. The menu bar could only open the sessions lane generically, not deep-link to the intended workload.
2. Session detail did not explicitly explain when opencode handoff was unavailable.

That meant the task's required deep-link and handoff diagnostics were not actually complete.

## What Didn't Work

The first pass assumed those behaviors already existed because earlier work notes said they did. Verifying against the current tree showed the opposite: the menu bar only rendered `Focus Sessions`, and the session detail pane only exposed `Attach: Pending`. The missing state had to be added in the monitoring model before the view layer could render it coherently.

## Solution

1. Add `menuBarSessionShortcuts` to the monitoring snapshot so the menu bar can render the leading workloads directly from the same ordered model the sessions lane uses.
2. Add `handoffUnavailableCopy` to `MonitoringSelectionDetail` so attachability failure becomes explicit operator-facing copy instead of an implied fact-card state.
3. Route menu bar deep links through a single helper that selects the sessions lane and workload together, then trigger an immediate refresh so the detail pane does not sit empty until the next poll.
4. Add regression tests for shortcut ordering, deep-link selection, reconnect reload, retained terminal detail, and handoff-unavailable copy.

## Prevention

- Treat ticket claims as untrusted until verified against the current branch state.
- Keep menu bar actions backed by monitoring-model state rather than hand-built view logic so selection order stays consistent.
- When a daemon contract field like `attachable` affects operator actionability, surface explicit copy instead of forcing the user to infer meaning from a boolean-shaped label.
