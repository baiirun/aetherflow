# Manual Daemon URL Resolution

**Date**: 2026-03-14  
**Task**: ts-27d3dd

## Context

This ticket changes the daemon-addressing contract so manual mode no longer depends on project-derived URLs by default. The requirement from planning was:

- manual mode should be global-by-default because sessions are created with `af spawn`
- auto mode should remain project-scoped
- explicit `listen_addr` must continue to win
- explicit `--project` on client commands must keep working as an intentional project-scoped target override

This work unblocks the macOS follow-up ticket that will point the globally installed app at the default manual daemon URL.

## What Was Done

1. Updated daemon config defaults in `internal/daemon/config.go`
   - manual mode now defaults `listen_addr` to `protocol.DefaultDaemonURL`
   - auto mode still derives `listen_addr` from `protocol.DaemonURLFor(project)`

2. Updated CLI daemon resolution in `cmd/af/cmd/root.go`
   - explicit `--project` now always routes to the project-scoped daemon URL
   - explicit or configured `listen_addr` still wins over derived defaults
   - config-driven auto mode still routes by project
   - manual mode without explicit targeting falls back to the global daemon URL

3. Added CLI hint flags in `cmd/af/cmd/daemon.go`
   - `af daemon` and `af daemon stop` now accept `--spawn-policy` as a resolver hint for lifecycle commands

4. Expanded regression coverage
   - `cmd/af/cmd/root_test.go` covers manual default, auto config routing, explicit `--project`, and `listen_addr` precedence
   - `internal/daemon/config_test.go` covers manual-vs-auto default `listen_addr`

5. Updated `README.md`
   - documents the split between global manual defaults and project-scoped auto defaults
   - documents that explicit `--project` still targets project-scoped daemons for client commands

## What Was Tried That Didn't Work

The first resolver change made `--project` conditional on spawn policy resolving to `auto`. That looked consistent with the new manual default, but it broke operator flows like `af status --project myapp` and `af logs --project myapp` when no config file was present. The fix was to separate explicit client targeting from default manual-mode addressing.

## Key Decisions

- Kept manual mode global by default instead of adding another project requirement, because manual sessions are created via `af spawn`
- Preserved explicit `--project` as a transport override so existing project-targeted CLI flows do not regress
- Kept `listen_addr` as the highest-priority explicit override to preserve current escape hatches and config compatibility

## Files Modified

- `internal/daemon/config.go`
- `internal/daemon/config_test.go`
- `cmd/af/cmd/root.go`
- `cmd/af/cmd/root_test.go`
- `cmd/af/cmd/daemon.go`
- `README.md`

## Verification

- `go test ./...`
- `git diff --check`

## Next Work

The next ticket is `ts-2df50e`, which should make the macOS Control Center default to the global manual daemon target instead of requiring project-derived endpoint setup.
