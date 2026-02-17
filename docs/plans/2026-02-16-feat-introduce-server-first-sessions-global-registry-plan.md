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

### Server-first observability findings (2026-02-16 spike)

Tested opencode 1.2.6 in `serve` mode on `http://127.0.0.1:4097`.

#### JSONL stdout is dead in attach mode

`opencode run --attach <url> --format json "prompt"` produces **zero bytes to stdout**. The `--format json` flag has no effect when `--attach` is used. This means:

- `ParseSessionID` (polls JSONL log file for `sessionID`) never finds data.
- `ParseToolCalls` (reads JSONL for tool_use events) returns empty.
- `FormatLogLine` (used by `af logs` and TUI log stream) has nothing to format.
- `captureSessionFromLog` (goroutine polling log file) exhausts retries silently.

All event data is delivered exclusively through the server's SSE stream and REST API.

#### SSE event stream (`GET /event`)

Server-scoped (not session-scoped). Returns `text/event-stream` with these event types:

| Event type | Payload | Frequency |
|---|---|---|
| `server.connected` | `{}` | Once on connect |
| `session.created` | `properties.info.id`, full session metadata | Once per session |
| `session.updated` | Full session metadata (title, summary, etc.) | Multiple per session |
| `session.status` | `properties.sessionID`, `status.type` (`busy`, `idle`, `retry`) | Frequent |
| `message.updated` | Full message info (role, tokens, cost, model) | Per message |
| `message.part.updated` | `properties.part` (text, tool call data) | Per part |
| `session.diff` | File diff data | Per step |

Session ID appears in the first `session.created` event as `properties.info.id` (format: `ses_<id>`).

**Limitations:** No backfill — only events after the SSE connection opens. No per-session filtering — all sessions on the server emit to the same stream.

#### REST API endpoints

The server exposes a REST API alongside the web UI (SPA fallback catches unknown routes):

| Endpoint | Method | Returns |
|---|---|---|
| `/session` | GET | List all sessions (JSON array) |
| `/session/:id` | GET | Session metadata (title, timestamps, permissions) |
| `/session/:id/message` | GET | Full message tree with parts (text, tool, step-finish) |
| `/session` | POST | Create session, returns `{id, slug, ...}` |
| `/event` | GET | SSE event stream |

`GET /session/:id/message` returns the same structured data as the SQLite `part.data` column. Tool parts include: `tool`, `state.status`, `state.input`, `state.output`, `state.time.start/end`, `state.title`. This is equivalent to what `ParseToolCalls` extracts from JSONL, but richer (includes output text).

Tested with historical sessions: a 375-tool-call session returned 4.5MB of JSON. Response size is proportional to session activity.

**Endpoints that return SPA fallback (not API):** `/health`, `/stats`, `/config`, `/session/:id/status`, `/session/:id/summary`, `/session/:id/export`, `/session/:id/part`.

#### SQLite database (`~/.local/share/opencode/opencode.db`)

Schema: `session`, `message`, `part`, `project`, `permission`, `todo`, `control_account`, `session_share`.

- `part.data` is a JSON column with `type` field: `text`, `tool`, `step-start`, `step-finish`, `patch`, `file`, `compaction`, `reasoning`, `agent`, `subtask`.
- Tool parts match the REST API shape exactly (the server reads from this DB).
- WAL mode, safe for concurrent reads.
- Global scope — all projects on the machine share one DB.
- `opencode export <session-id>` CLI reads this DB directly (works without a running server).

#### CLI mechanisms

- `opencode export <session-id>` — dumps session as JSON (same shape as `/session/:id/message`). No server needed.
- `opencode session list` — lists sessions. No server needed.
- `opencode attach <url>` — TUI attached to a running server. Supports `--session <id>` for resuming.

#### Observability mechanism comparison

| Need | JSONL (current) | SSE | REST API | SQLite |
|---|---|---|---|---|
| Session ID capture | Dead in attach mode | `session.created` event | `POST /session` response | Query by recency |
| Live tool call stream | Dead in attach mode | `message.part.updated` | Poll `/session/:id/message` | Poll query |
| Tool call history | N/A | No backfill | Full history | Full history |
| Session status | Dead in attach mode | `session.status` events | `GET /session/:id` | Query DB |
| Cost/tokens | Dead in attach mode | `message.updated` events | In message info | In message data |

**Recommendation:** Use the REST API as the primary observability mechanism. It provides both real-time polling and historical data, is session-scoped, and returns structured tool data matching what `ParseToolCalls` and `FormatLogLine` already consume. SSE supplements for truly real-time streaming (TUI follow mode). JSONL log files should be retired from the observability path.

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

- Server-first launch: `opencode run --attach <url>` (note: `--format json` has no effect in attach mode — stdout is empty; observability comes from the server's REST API and SSE stream instead)
- Resume path: add `--session <id>` when needed

This reduces brittle command mutation in `pool.go` and `spawn.go`.

#### 4) Task source boundary (incremental)

Introduce a minimal `WorkSource` boundary early (claim/get/update/log) so new session-first lifecycle code does not bake in prog semantics.
Keep prog as the first adapter implementation.

## Implementation Phases

### Phase A: Ship server-first runtime + minimal registry

Goal: ship user-visible value quickly with low complexity.

- Replace daemon/spawn launch paths with server-first attach launch (`opencode run --attach <url>`).
- Remove legacy standalone launch path from daemon/spawn flows.
- Add minimal global session registry package and persistence.
- Capture session ID from `session.created` SSE event or `POST /session` API response (JSONL stdout is empty in attach mode — cannot be used as a fallback).
- Replace JSONL-based observability (`ParseToolCalls`, `ParseSessionID`, `FormatLogLine`, `af logs` file tailing, TUI `readLogLines`) with server REST API queries (`GET /session/:id/message`). SSE stream supplements for real-time follow mode.
- Add `af sessions` + `af session attach <session-id>` (plus disambiguation when `session_id` collides across `server_ref`).

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
- [ ] Status/logs (`af status`, `af logs`, TUI) work with server-first launch via REST API polling (`GET /session/:id/message`) replacing JSONL log file reads. (JSONL stdout is empty in attach mode — original assumption was wrong.)
- [x] Attach/detach works on active sessions without ending the underlying session.
- [ ] Session identity is captured from server API (`session.created` SSE event or `POST /session` response) rather than JSONL log parsing. (JSONL-based `ParseSessionID` cannot work in attach mode.)
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

### Server API unavailability for observability

- Since status/logs now depend on the opencode server API instead of local log files, a server crash or network issue makes observability data unavailable.
- Mitigation: the daemon should cache the last-known session state and tool call summary in memory. If the server API is unreachable, `af status` falls back to cached data with a staleness indicator. Historical data remains available via SQLite (`~/.local/share/opencode/opencode.db`) as a last resort.

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

Observability code affected by JSONL→API migration:

- `internal/daemon/jsonl.go` — `ParseSessionID`, `ParseToolCalls` (both dead in attach mode)
- `internal/daemon/logfmt.go` — `FormatLogLine` (needs input from API instead of JSONL)
- `internal/daemon/status.go:239-268` — `BuildAgentDetail` calls `ParseToolCalls` + `ParseSessionID` on log files
- `internal/daemon/status.go:300-326` — `buildSpawnDetail` same pattern
- `internal/daemon/pool.go:556-616` — `captureSessionFromLog` polls log file for session ID
- `internal/daemon/spawn_rpc.go:112-149` — `captureSpawnSession` same pattern
- `cmd/af/cmd/logs.go` — `tailFile` reads/follows JSONL log file directly
- `internal/tui/logstream.go` — `readLogLines` reads JSONL log file for TUI

### External

- https://builders.ramp.com/post/why-we-built-our-background-agent
