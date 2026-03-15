# macOS Manual Daemon Targeting

**Date**: 2026-03-14  
**Task**: ts-2df50e

## Context

This ticket follows `ts-27d3dd`, which made manual daemon addressing global by default in the CLI/backend. The macOS Control Center needed to adopt that contract so a globally installed app can connect to manual mode without knowing a project-derived daemon URL or depending on repo cwd.

## What Was Done

1. Updated bootstrap resolution in `macos/ControlCenter/Sources/AetherflowControlCenter/ShellBootstrap.swift`
   - default daemon target is now the global manual endpoint
   - explicit loopback overrides still win
   - bootstrap now records the daemon target source, reason, and normalized listen address

2. Updated app-triggered start in `macos/ControlCenter/Sources/AetherflowControlCenter/DaemonControl.swift`
   - app now starts the daemon with `--spawn-policy manual`
   - app forwards `--listen-addr` matching the resolved daemon target so start/probe/monitoring stay aligned

3. Added CLI support in `cmd/af/cmd/daemon.go`
   - `af daemon start` now accepts `--listen-addr`
   - detached re-exec forwards the flag correctly

4. Expanded diagnostics and operator visibility
   - transport/monitoring notes explain why the app chose its daemon endpoint
   - lifecycle and monitoring notes now call out non-manual daemons and daemon URL mismatches
   - diagnostics view renders the daemon target reason directly

5. Added regression coverage
   - Swift tests cover global manual defaults, explicit overrides, start invocation, and mismatch diagnostics
   - Go test covers `buildConfig` honoring `--listen-addr`

## What Was Tried That Didn't Work

The original app behavior reused project hashing from the CLI’s old auto-oriented path. That was the root of the unreachable-daemon report: the app and daemon could both be “correct” relative to their own targeting rules while still speaking to different endpoints. The fix had to align the start path as well as the read path.

## Key Decisions

- The app now treats manual mode as the default operator contract; project name remains display context, not the default transport key.
- Explicit loopback daemon overrides remain supported, but they are treated as manual endpoint overrides, not as a signal to re-enter project hashing.
- A small CLI addition (`--listen-addr`) was included because it is the simplest way to preserve alignment during app-triggered daemon starts.

## Files Modified

- `macos/ControlCenter/Sources/AetherflowControlCenter/ShellBootstrap.swift`
- `macos/ControlCenter/Sources/AetherflowControlCenter/DaemonControl.swift`
- `macos/ControlCenter/Sources/AetherflowControlCenter/ShellModels.swift`
- `macos/ControlCenter/Sources/AetherflowControlCenter/ShellStores.swift`
- `macos/ControlCenter/Sources/AetherflowControlCenter/MonitoringStore.swift`
- `macos/ControlCenter/Sources/AetherflowControlCenter/ControlCenterRootView.swift`
- `macos/ControlCenter/Tests/AetherflowControlCenterTests/ShellBootstrapTests.swift`
- `macos/ControlCenter/Tests/AetherflowControlCenterTests/DaemonControlTests.swift`
- `macos/ControlCenter/Tests/AetherflowControlCenterTests/DaemonLifecycleStoreTests.swift`
- `macos/ControlCenter/Tests/AetherflowControlCenterTests/MonitoringStoreTests.swift`
- `cmd/af/cmd/daemon.go`
- `cmd/af/cmd/daemon_test.go`
- `MATRIX.md`

## Verification

- `swift test`
- `go test ./cmd/af/cmd`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `golangci-lint run`
- `git diff --check`
