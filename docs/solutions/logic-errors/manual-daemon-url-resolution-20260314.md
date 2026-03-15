---
module: cli
date: 2026-03-14
problem_type: logic_error
component: daemon-addressing
symptoms:
  - "manual daemons still resolve to project-hashed URLs even though manual mode does not require a project"
  - "the macOS app and CLI disagree about which daemon endpoint to use unless callers precompute a project-derived URL"
  - "changing manual mode to global can accidentally break explicit --project targeting on client commands"
root_cause: daemon endpoint resolution conflated default manual-mode addressing with explicit project-targeted client routing
resolution_type: code_fix
severity: medium
tags:
  - daemon
  - cli
  - macos
  - endpoint-resolution
  - manual-mode
  - auto-mode
  - project-routing
---

# Manual Daemon URL Resolution

## Problem

The daemon contract treated manual mode as project-optional in config and behavior, but still derived the endpoint from project identity. That forced callers to know a project-scoped URL even when manual mode only needed a single global daemon that serves `af spawn` requests.

## Symptoms

- `af daemon start --spawn-policy manual` still defaulted to a project-hashed URL when a project was present
- a globally installed macOS app had to know repo-local project details or an explicit `AETHERFLOW_DAEMON_URL`
- a naive resolver change risked breaking existing flows like `af status --project myapp`, which operators use to intentionally target project-scoped auto daemons

## What Didn't Work

The first resolver pass treated `--project` as meaningful only when spawn policy resolved to `auto`. That preserved the new manual default, but it regressed client commands that relied on `--project` as the only explicit routing hint.

## Solution

Split the contract into two separate decisions:

1. **Daemon startup defaults**
   - Manual mode defaults to the global daemon URL: `protocol.DefaultDaemonURL`
   - Auto mode keeps project-scoped defaults via `protocol.DaemonURLFor(project)`
   - Explicit `listen_addr` still wins

2. **Client-side explicit routing**
   - Explicit `--project` remains an intentional override for client commands
   - Config-driven auto mode still routes to the project-scoped URL
   - Manual mode without explicit targeting falls back to the global daemon URL

## Why This Works

This preserves the intended manual-mode architecture without breaking the established operator contract for project-scoped inspection commands. The result is:

- zero-config manual daemon startup resolves to one global endpoint
- auto daemons stay isolated per project
- operators can still reach a specific project-scoped daemon with `--project`
- future macOS work can default to the global manual endpoint without requiring precomputed URLs

## Prevention

- Treat **default addressing** and **explicit operator targeting** as separate concerns in endpoint resolvers
- When changing a shared resolver, test both daemon lifecycle commands and read-only client commands that reuse the same routing path
- Keep the routing priority order explicit: `--project` override, explicit `listen_addr`, config-driven auto routing, manual default
