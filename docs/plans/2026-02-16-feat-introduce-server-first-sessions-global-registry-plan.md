---
title: "feat: introduce server-first sessions with global registry and attach/detach routing"
type: feat
date: 2026-02-16
brainstorm: docs/brainstorms/2026-02-16-server-first-sessions-brainstorm.md
---

# Introduce server-first sessions with global registry and attach/detach routing

## Overview

Move aetherflow from process-first agent execution to server-first session orchestration. The daemon should manage or target opencode servers, agents should run as clients (`opencode run --attach`), and humans should be able to attach/detach to the same session (`opencode attach --session`) without killing the session.

This plan establishes sessions as the primary identity and introduces a global aetherflow session registry for discovery and routing across local and remote servers.

## Problem Statement

Current lifecycle is task/process-centric:

- Daemon spawns standalone `opencode run --format json` child processes.
- Session IDs are parsed from JSONL logs and are not first-class in daemon state.
- Human iteration requires kill/respawn patterns.
- Discovery is local, task-first, and coupled to prog-centric flows.

This blocks the roadmap direction in `README.md:494` (custom task sources, remote runtimes, broader trigger surfaces) because the orchestration primitive is still task/process rather than session.

## Proposed Solution

Introduce a server-first session model in phases:

1. Keep opencode as the source of session content/history.
2. Add a global aetherflow session registry for routing/discovery metadata.
3. Add session-oriented CLI surface (`af sessions`, `af session attach ...`) while preserving pool-control command names.
4. Switch daemon and spawn launch paths to server-first (`opencode run --attach`) as the only supported mode.
5. Refactor daemon lifecycle (spawn/reap/respawn/reclaim) to be session-aware.
6. Decouple task source access from prog via interfaces; keep prog adapter as default implementation.

## Research Summary

### Local findings used for this plan

- Existing spawn default and config are process-first: `internal/daemon/config.go:17`, `internal/daemon/config.go:83`.
- Daemon and CLI currently wire a static spawn command string: `cmd/af/cmd/daemon.go:62`, `cmd/af/cmd/daemon.go:162`, `cmd/af/cmd/spawn.go:129`.
- Pool lifecycle is task-keyed and process-keyed: `internal/daemon/pool.go:271`, `internal/daemon/pool.go:356`, `internal/daemon/pool.go:438`.
- Session ID extraction is log-derived (`ParseSessionID`): `internal/daemon/jsonl.go:151`.
- Status/logs are task/log-path centric: `internal/daemon/status.go:176`, `internal/daemon/logs.go:20`.
- `af resume` already exists for pool mode control: `cmd/af/cmd/pool_control.go:54`.
- Prog coupling hotspots: `internal/daemon/poll.go:151`, `internal/daemon/reconcile.go:85`, `internal/daemon/reconcile.go:131`, `internal/daemon/status.go:343`.

### External decision

No additional external research required for this plan. We already validated opencode behavior directly with local spikes and have a matching architecture reference from Ramp's background agent writeup.

## Architecture Decisions

### Session identity and ownership

- **Canonical handle:** `session_ref = {server_ref, session_id}`.
- **User-facing handle:** `session_id` shorthand, only when unambiguous.
- **opencode owns:** conversation history and session internals.
- **aetherflow owns:** routing and orchestration metadata (where to attach, what spawned it, associated work item context).

### Global registry location

- Store in `~/.config/aetherflow/sessions/`.
- Global scope (not project-local) so remote/local sessions can be discovered consistently.

### CLI naming

Because `af resume` already means pool resume, introduce explicit session namespace:

- `af sessions` (list/filter)
- `af session attach <session-id>`

`af session show <session-id>` is optional follow-up if list output is not enough.

Keep existing `af resume` pool-control behavior unchanged.

### Source-of-truth precedence

To avoid nondeterministic routing:

1. Live probe/attach check against recorded server URL
2. In-memory daemon cache
3. Registry snapshot on disk

### Migration guardrail

Legacy execution is deprecated as part of this project. There are no active sessions to cut over, so we switch directly to server-first and keep rollback as a release rollback only.

## Technical Approach

### New core components

#### 1) Session registry package

Add `internal/daemon/sessions/` for metadata index, with:

- Schema versioning (`schema_version`)
- Atomic writes (`write temp + rename`)
- Basic read/write/list and heartbeat updates
- Stale marking by timestamp (advanced lease/quarantine deferred)

Initial record shape:

- `server_ref` (URL or named target)
- `session_id`
- `directory`
- `project` (optional)
- `origin_type` (`pool`, `spawn`, `manual`)
- `work_ref` (task/work item id, optional, backend-agnostic)
- `agent_id` (optional)
- `status` (`active`, `idle`, `terminated`, `stale`)
- `created_at`, `last_seen_at`, `updated_at`

#### 2) Server manager

Add daemon-managed opencode server lifecycle support:

- Start/health/stop and reconnect logic for local managed server
- External/remote targets are read-only attach targets in v1 (no remote stop/start)
- Credential handling via env/config (never logged)

#### 3) Launch spec builder

Replace ad-hoc `spawn_cmd` string handling with a structured launch builder that supports:

- Server-first launch: `opencode run --attach <url> --format json`
- Resume path: add `--session <id>` when needed

This reduces brittle command mutation in `pool.go` and `spawn.go`.

#### 4) Task source boundary (incremental)

Introduce a minimal `WorkSource` boundary early (claim/get/update/log) so new session-first lifecycle code does not bake in prog semantics.
Keep prog as the first adapter implementation.

## Implementation Phases

### Phase A: Ship server-first runtime + minimal registry

Goal: ship user-visible value quickly with low complexity.

- Replace daemon/spawn launch paths with server-first attach launch (`opencode run --attach <url> --format json`).
- Remove legacy standalone launch path from daemon/spawn flows.
- Add minimal global session registry package and persistence.
- Register sessions from launch handshake/output events; JSONL remains observability fallback only.
- Add `af sessions` + `af session attach <session-id>` (plus disambiguation when `session_id` collides across `server_ref`).
- Keep JSONL log capture and status/log display behavior compatible.

Acceptance target: users can discover and attach/detach sessions while server-first is the only runtime path.

### Phase B: Harden and extend

- Add stronger concurrency controls (CAS/lease/fencing) only where required by observed contention.
- Add remote target support and stricter trust policy (allowlist/TLS/auth handling).
- Refine session-aware respawn/reclaim behavior and richer stale-state handling.
- Expand WorkSource abstraction and prompt/status decoupling from prog where needed.

Acceptance target: server-first runtime is robust under failure, multi-actor access, and remote targets.

## Acceptance Criteria

- [x] Session-oriented CLI commands exist without breaking current pool control commands.
- [x] Global session registry persists and resolves routing metadata with canonical key `{server_ref, session_id}`.
- [x] Registry writes are atomic and safe under concurrent daemon/CLI access.
- [x] Server-first launch is the only runtime path for daemon and spawn flows.
- [x] JSONL-based status/logs continue to work with server-first launch.
- [x] Attach/detach works on active sessions without ending the underlying session.
- [x] Session identity is captured from launch handshake/output events, with tolerant fallback parsing when needed.
- [x] Minimal WorkSource boundary is in place before deeper session lifecycle refactors.
- [ ] Security controls exist for attach targets and credentials (permissions, redaction, validation, URL trust policy).
- [ ] Plan defines explicit release rollback path.

## Failure Modes and Mitigations

### Registry drift

- Mitigation: precedence policy (`live probe > cache > registry`) and heartbeat timestamps; advanced quarantine policy deferred.

### Partial launch failures

- Mitigation: explicit two-phase launch transaction (start client, verify/capture session id, then mark active in registry).

### Concurrency races (daemon reap vs human attach)

- Mitigation: idempotent attach behavior and bounded lock duration in v1; stronger lease/fencing protocol in Phase B.

### Remote server auth/config leakage

- Mitigation: strict redaction in logs, secure file modes (`0700` dirs, `0600` files), env-based secret ingestion, trusted URL policy.

### UX ambiguity from command collisions

- Mitigation: keep `af resume` unchanged; introduce `af session attach` namespace.

## Success Metrics

- Attach success rate for known sessions
- Session lookup latency (`af sessions`)
- Registry stale-entry rate
- Crash-recovery success rate in server-first mode
- Regressions in `af status`/`af logs` behavior (must be zero for existing flows)

## Dependencies and Prerequisites

- opencode version that supports `serve`, `run --attach`, `attach --session`
- daemon config support for server runtime and attach target configuration
- CLI namespace additions for session commands

## Risks

- Hidden prog coupling in status/prompt/tooling paths
- Operational complexity around remote URLs/auth and stale registry cleanup

## Documentation Plan

- Update `README.md` architecture and CLI sections with session-first model.
- Add session registry docs (location, schema, troubleshooting).
- Add release notes for legacy-path deprecation and server-first behavior.

## References

### Internal

- `docs/brainstorms/2026-02-16-server-first-sessions-brainstorm.md`
- `internal/daemon/config.go:17`
- `internal/daemon/pool.go:271`
- `internal/daemon/pool.go:356`
- `internal/daemon/pool.go:438`
- `internal/daemon/jsonl.go:151`
- `internal/daemon/status.go:176`
- `internal/daemon/logs.go:20`
- `cmd/af/cmd/spawn.go:129`
- `cmd/af/cmd/pool_control.go:54`
- `internal/daemon/poll.go:151`
- `internal/daemon/reconcile.go:85`

### External

- https://builders.ramp.com/post/why-we-built-our-background-agent
