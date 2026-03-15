# macOS Monitoring Diagnostics

**Date**: 2026-03-15  
**Task**: ts-9fb41d

## Context

This ticket was to make the reduced macOS Control Center trustworthy under daemon churn by adding diagnostics and regression coverage for connect, reconnect, start/stop, deep-link, handoff, and daemon-targeting behavior. When resumed after the manual-daemon follow-up work, the branch had already picked up reconnect handling, retained terminal detail, lifecycle diagnostics, and daemon-target mismatch coverage from `main`, but the required menu bar deep-link and explicit handoff-unavailable surfaces were still missing.

## What Was Done

1. Added monitoring-model surfaces in `macos/ControlCenter/Sources/AetherflowControlCenter/MonitoringStore.swift`
   - `menuBarSessionShortcuts` exposes the leading workloads in the same order the sessions lane uses
   - `handoffUnavailableCopy` turns `attachable=false` into explicit operator-facing copy
   - `activateMenuBarSessionDeepLink(...)` centralizes the session-lane selection behavior

2. Updated the UI in `macos/ControlCenter/Sources/AetherflowControlCenter/ControlCenterRootView.swift`
   - menu bar now renders up to three session shortcuts
   - deep-link actions select the workload, switch to the sessions lane, and force an immediate refresh
   - session detail now renders an `Opencode handoff unavailable` card when the daemon marks the session unattached

3. Added regression coverage in `macos/ControlCenter/Tests/AetherflowControlCenterTests/MonitoringStoreTests.swift`
   - menu bar shortcuts expose the leading ordered workloads
   - menu bar deep-link selects the correct session detail
   - handoff-unavailable copy is surfaced when `attachable` is false

4. Updated `MATRIX.md`
   - reconnect reload behavior
   - retained terminal detail behavior
   - menu bar deep-link and handoff-unavailable behavior

## What Was Tried That Didn't Work

The first patch attempt failed because the selection-detail type was mislocated in memory: `MonitoringSelectionDetail` lives in `MonitoringStore.swift`, not `ShellModels.swift`. Splitting the patch by file fixed that quickly.

The original assumption that menu bar deep links already existed was wrong relative to current `main`. Verifying the actual view tree showed only a generic `Focus Sessions` button, so the missing work had to be implemented rather than just checked off.

During live smoke verification, an isolated daemon on `127.0.0.1:7199` exposed a separate CLI endpoint-resolution inconsistency: explicit daemon URL overrides did not resolve consistently across `start`, `status`, and `stop`. That was split into `ts-3db986` so it would not block closing this app-focused task.

## Key Decisions

- Keep the deep-link logic in the monitoring model/store layer so menu bar behavior stays aligned with the sessions lane selection model.
- Treat handoff unavailability as a first-class diagnostic message, not just an implied boolean in a fact card.
- Trigger an immediate refresh on menu bar deep-link to avoid a blank detail pane while waiting for the next poll interval.

## Files Modified

- `macos/ControlCenter/Sources/AetherflowControlCenter/MonitoringStore.swift`
- `macos/ControlCenter/Sources/AetherflowControlCenter/ControlCenterRootView.swift`
- `macos/ControlCenter/Tests/AetherflowControlCenterTests/MonitoringStoreTests.swift`
- `MATRIX.md`
- `docs/solutions/logic-errors/macos-monitoring-diagnostics-regression-20260315.md`
- `docs/solutions/learnings.md`
- `docs/solutions/handoffs/macos-monitoring-diagnostics-20260315.md`

## Verification

- `cd macos/ControlCenter && swift test`
- `go test ./...`
- `go build ./...`
- `golangci-lint run`
- `git diff --check`
- Live smoke:
  - `go build -o /tmp/aetherflow-af ./cmd/af`
  - `/tmp/aetherflow-af daemon start --spawn-policy manual --listen-addr 127.0.0.1:7199 --detach`
  - `AETHERFLOW_DAEMON_URL=http://127.0.0.1:7199 /tmp/aetherflow-af status`
  - `cd macos/ControlCenter && AETHERFLOW_CLI_PATH=/tmp/aetherflow-af AETHERFLOW_DAEMON_URL=http://127.0.0.1:7199 swift run AetherflowControlCenter`
