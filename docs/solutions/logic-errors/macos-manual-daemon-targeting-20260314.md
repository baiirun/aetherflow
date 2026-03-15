---
module: macos-control-center
date: 2026-03-14
problem_type: logic_error
component: daemon-targeting
symptoms:
  - "the macOS app reports the daemon as unreachable even though the daemon started successfully"
  - "manual-mode monitoring requires operators to know a project-hashed daemon URL ahead of time"
  - "app-triggered daemon start can launch a daemon on a different endpoint than the app probes"
root_cause: the app reused project-scoped daemon hashing for manual-mode defaults and did not preserve explicit endpoint overrides during daemon start
resolution_type: code_fix
severity: medium
tags:
  - macos
  - control-center
  - daemon
  - manual-mode
  - bootstrap
  - diagnostics
---

# macOS Manual Daemon Targeting

## Problem

The globally installed Control Center app was deriving its daemon endpoint the same way auto mode does: from repo/project identity. That is the wrong default for manual mode, where the operator should be able to open the app and connect to the single global manual daemon without precomputing a project-specific URL.

## What Didn't Work

The app already had explicit daemon URL overrides, but the default path still hashed the project name. That meant the app could probe one loopback URL while `af daemon start` brought up a daemon on another. Fixing only the probe side would still leave app-triggered start misaligned.

## Solution

1. Make the app default to the global manual daemon URL (`http://127.0.0.1:7070`) unless an explicit loopback override is provided.
2. Track why the daemon target was chosen, and surface that reason in transport and diagnostics views.
3. Start the daemon with `af daemon start --detach --spawn-policy manual --listen-addr <resolved target>` so app-triggered start, lifecycle probing, and monitoring all converge on the same endpoint.
4. Add `--listen-addr` support to `af daemon start` so the macOS app can preserve explicit endpoint overrides without inventing temp config files.

## Prevention

- Treat the macOS app as a global operator tool, not a repo-local shell wrapper.
- Keep manual daemon defaults project-agnostic; only explicit endpoint overrides should change the target.
- When the app chooses a daemon endpoint, record both the chosen URL and the reason so mismatch reports are debuggable.
