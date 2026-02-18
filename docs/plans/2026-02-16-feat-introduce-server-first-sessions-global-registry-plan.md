---
title: "feat: introduce server-first sessions with global registry and attach/detach routing"
type: feat
date: 2026-02-16
brainstorm: docs/brainstorms/2026-02-16-server-first-sessions-brainstorm.md
---

# Introduce server-first sessions with global registry and attach/detach routing

## Overview

Move aetherflow from process-first agent execution to server-first session orchestration. The daemon manages opencode servers, agents run as clients, and humans can attach/detach to sessions without killing them.

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
5. Replace JSONL-based observability with a plugin event pipeline from the opencode server.
6. Refactor daemon lifecycle (spawn/reap/respawn/reclaim) to be session-aware.
7. Decouple task source access from prog via interfaces; keep prog adapter as default implementation.

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

All event data is delivered exclusively through the server's SSE stream, REST API, and plugin system.

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
| `/session/:id/prompt_async` | POST | Fire prompt asynchronously, returns 204 |
| `/session/:id/message` | POST | Send message synchronously, returns response |
| `/session/status` | GET | Status for all sessions (`{[id]: {type}}`) |
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

### Plugin event pipeline findings (2026-02-17 spike)

Investigated opencode's plugin system, SDK, and server API docs (`opencode.ai/docs/sdk/`, `opencode.ai/docs/server/`, `opencode.ai/docs/plugins/`). Discovered that the plugin system provides in-process event subscriptions that cover all observability needs without requiring REST polling or SSE connections. Validated with four spikes.

#### Spike 1: Plugin event inspection

Wrote a minimal plugin at `~/.config/opencode/plugins/aetherflow-event-spike.ts` that logs every event to a JSONL file. Ran an agent session (`opencode run --attach` against `opencode serve`).

**Result:** 58 events from a single "hello + one bash tool call" session. Event types received:

| Event type | Count | Session ID location | Key data |
|---|---|---|---|
| `session.created` | 1 | `properties.info.id` | Full session metadata, slug, permissions, timestamps |
| `session.updated` | 6 | `properties.info.id` | Title changes, summary (additions/deletions/files) |
| `session.status` | 6 | `properties.sessionID` | `{type: "busy"}` or `{type: "idle"}` |
| `session.idle` | 1 | `properties.sessionID` | Explicit completion signal |
| `session.diff` | 3 | `properties.sessionID` | File diff array |
| `message.updated` | 8 | `properties.info.sessionID` | Role, tokens, cost, model, finish reason |
| `message.part.updated` | 10 | `properties.part.sessionID` | Text content, tool calls with full state lifecycle |
| `message.part.delta` | 14 | `properties.sessionID` | Streaming text deltas (token-by-token, not in original SSE research) |

Tool call lifecycle from `message.part.updated` events:
1. `status: "pending"` — tool identified, input empty
2. `status: "running"` — input populated (`{command, description}`)
3. `status: "running"` — output appearing (`metadata.output`)
4. `status: "completed"` — full output, exit code, `time.start`/`time.end`, title

This provides **more data than JSONL ever did** — pending state, running with partial output, completion with exit code. The `message.part.delta` event type gives real-time token-by-token text streaming.

Session ID locations vary by event type but are consistently extractable:
- `session.created/updated`: `properties.info.id`
- `session.status/idle/diff`: `properties.sessionID`
- `message.updated`: `properties.info.sessionID`
- `message.part.updated`: `properties.part.sessionID`
- `message.part.delta`: `properties.sessionID`

#### Spike 2: Plugin → daemon RPC over Unix socket

Extended the spike plugin to connect to the daemon's Unix socket and send events using the existing JSON-RPC protocol (`{method: "session.event", params: {...}}`).

**Environment variable propagation:** Env vars set on the `opencode serve` process are visible to plugins. Env vars set on `opencode run --attach` are NOT visible to plugins (plugins run in the server process, not the client). The daemon must set `AETHERFLOW_SOCKET` and `AETHERFLOW_AGENT_ID` when spawning the server.

**Socket connectivity:** Bun's `net.createConnection` connects to Unix domain sockets. Tested against a mock daemon listener. All 27 events from a session arrived with correct `agent_id`, `event_type`, `session_id`, and full `data` payload. The existing JSON-RPC protocol works unchanged — the plugin sends `{method, params}` and the daemon responds with `{success, result, error}`.

**Minor issue:** Calling `socket.end()` after writing closes the read side before the response arrives. Production implementation should either wait for the response before closing, or fire-and-forget for non-critical events.

#### Spike 3: Server-attached sessions via REST API

Tested the full lifecycle of creating and interacting with sessions via the REST API instead of spawning `opencode run --attach` processes.

| Test | Endpoint | Result |
|---|---|---|
| Pre-create session | `POST /session` with `{"title": "..."}` | Returns full session object with `id` field immediately. Deterministic — no polling or race condition. |
| Async prompt | `POST /session/:id/prompt_async` | Returns 204 No Content. Agent runs to completion in the background with no client attached. |
| Session survives detach | Check `GET /session/:id/message` after agent finishes | Full message tree present: user prompt, tool call (bash with output), assistant response. |
| Follow-up message | `POST /session/:id/message` (synchronous) | Session retains full context. Agent answered correctly about prior interaction. |
| CLI re-attach | `opencode run --attach <url> --session <id>` | Attaches to existing session with full history. |

**Key finding:** `POST /session` + `POST /session/:id/prompt_async` can replace the entire `opencode run --attach <url> --format json "prompt"` launch path. The daemon would become a pure API orchestrator. This is a separate follow-up from the plugin event pipeline work.

#### Spike 4: Session ID capture approaches

Two approaches validated, both reliable:

| Approach | Mechanism | Latency | Race safety | Validated in |
|---|---|---|---|---|
| Plugin `session.created` event | Plugin receives event with `properties.info.id` | ~2ms after session creation | Safe if single agent per server; needs session-to-agent correlation with multiple | Spike 1 |
| `POST /session` pre-creation | Create session via API first, pass `--session <id>` to agent | 0ms (ID known before agent starts) | Fully deterministic, no race | Spike 3 |

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

### Observability via plugin event pipeline

Use an opencode plugin as the primary observability mechanism:

- Plugin subscribes to all server events in-process (zero network overhead).
- Plugin pushes events to the daemon via its existing Unix socket RPC (`session.event` method).
- Plugin is configured via env vars (`AETHERFLOW_SOCKET`, `AETHERFLOW_AGENT_ID`), set by the daemon when spawning the opencode server. If absent, plugin is inert — safe for standalone opencode use.
- Plugin forwards all events unfiltered; daemon decides what to keep and index.
- Daemon stores events in an in-memory per-agent ring buffer.
- REST API (`GET /session/:id/message`) serves as backfill source on daemon restart.

This replaces the originally proposed REST/SSE polling approach. See `docs/brainstorms/2026-02-17-plugin-event-pipeline-brainstorm.md` for full design rationale and spike details.

### Session ID capture

Use plugin `session.created` event for session ID capture. The event fires ~2ms after session creation and contains `properties.info.id`. This replaces `captureSessionFromLog` (JSONL polling) which is dead in attach mode.

`POST /session` pre-creation is the superior long-term approach (deterministic, no race) but requires launch path changes. Deferred to the API orchestration migration.

### Migration guardrail

Legacy execution is deprecated as part of this project. There are no active sessions to cut over, so we switch directly to server-first and keep rollback as a release rollback only.

## Technical Approach

### New core components

#### 1) Session registry package (SHIPPED — Phase A)

`internal/sessions/store.go` — global session registry with JSON file persistence at `~/.config/aetherflow/sessions/sessions.json`:

- Schema versioning (`schema_version`)
- Atomic writes (`write temp + rename`) with file locking (`flock`)
- `Upsert`, `List`, `SetStatusBySession`, `SetStatusByWorkRef`
- Records keyed by `{server_ref, session_id}`

Record shape:

- `server_ref` (URL or named target)
- `session_id`
- `directory`
- `project` (optional)
- `origin_type` (`pool`, `spawn`, `manual`)
- `work_ref` (task/work item id, optional, backend-agnostic)
- `agent_id` (optional)
- `status` (`active`, `idle`, `terminated`, `stale`)
- `created_at`, `last_seen_at`, `updated_at`

#### 2) Server manager (SHIPPED — Phase A)

`internal/daemon/server.go` — daemon-managed opencode server lifecycle:

- Starts `opencode serve --port <port>` if not already running
- TCP health polling for readiness
- Daemon supervises and restarts on crash
- `internal/daemon/server_url.go` — validates local-only URLs in v1

#### 3) Launch spec builder (SHIPPED — Phase A)

`internal/daemon/spawn_cmd.go` — `EnsureAttachSpawnCmd()`:

- Appends `--attach <serverURL>` to spawn command
- Server-first launch is the only supported mode

#### 4) Task source boundary (SHIPPED — Phase A)

`internal/daemon/work_source.go` — `WorkSource` interface with `Claim` and `GetMeta` methods. `ProgWorkSource` is the default adapter.

#### 5) Opencode plugin (`aetherflow-events`)

TypeScript plugin, installed globally at `~/.config/opencode/plugins/aetherflow-events.ts`.

- Reads `AETHERFLOW_SOCKET` and `AETHERFLOW_AGENT_ID` from environment
- If absent, returns empty hooks (inert)
- Subscribes to `event` hook, forwards all events to daemon RPC
- Extracts `session_id` from event payload (location varies by event type — see spike 1 findings)
- Handles socket errors gracefully (log warning, don't crash the agent)
- Fire-and-forget: sends events without waiting for response

#### 6) Daemon event handler + in-memory buffer

New `session.event` RPC handler and per-agent event storage:

- `session.event` handler receives events from the plugin
- Per-agent ring buffer (bounded, full payloads including tool output)
- Session ID extraction on `session.created` → updates agent state + session registry (replaces `captureSessionFromLog`)
- Session status tracking on `session.status`/`session.idle`/`session.error` events

#### 7) Refactored observability consumers

Replace JSONL file reads with event buffer reads:

- `BuildAgentDetail` → read tool calls from buffer instead of `ParseToolCalls` on JSONL file
- `buildSpawnDetail` → same
- `captureSessionFromLog` / `captureSpawnSession` → replaced by `session.created` event handler
- `af logs` → read from daemon RPC (new method to stream events) instead of `tailFile` on JSONL
- `af logs -f` → subscribe to new events from daemon instead of `followFile` on JSONL
- TUI `readLogLines` → read from daemon RPC instead of scanning JSONL file
- `FormatLogLine` → adapt to format plugin event payloads instead of JSONL bytes

#### 8) Environment variable injection

Daemon sets `AETHERFLOW_SOCKET` and `AETHERFLOW_AGENT_ID` on the `opencode serve` process environment (not on `opencode run --attach` — plugins run in the server, not the client).

#### 9) REST API client (minimal, for backfill)

Lightweight Go HTTP client for opencode REST API:

- `GET /session/:id/message` — fetch full message tree for backfill on daemon restart
- Reusable for future `POST /session` + `prompt_async` migration

## Implementation Phases

### Phase A: Server-first runtime + minimal registry (SHIPPED)

Shipped user-visible value: server-first launch, session registry, CLI commands.

- [x] Replaced daemon/spawn launch paths with server-first attach launch (`opencode run --attach <url>`).
- [x] Removed legacy standalone launch path from daemon/spawn flows.
- [x] Added global session registry package and persistence (`internal/sessions/store.go`).
- [x] Added `af sessions` + `af session attach <session-id>`.
- [x] Added managed opencode server lifecycle (`internal/daemon/server.go`).
- [x] Added `WorkSource` interface to decouple from prog.
- [x] Added server URL validation (local-only in v1).

**Known gap:** Observability (status/logs/TUI) still reads from JSONL files which are empty in attach mode. Session ID capture still uses JSONL polling which silently fails. Both addressed in Phase A.1.

### Phase A.1: Plugin event pipeline (CURRENT)

Goal: restore observability by replacing dead JSONL paths with the plugin event pipeline. All spike validations complete.

#### Step 1: Plugin + daemon event handler

- Write production `aetherflow-events.ts` plugin (based on validated spike plugin).
- Add `session.event` RPC handler to daemon.
- Add per-agent in-memory event buffer.
- Inject `AETHERFLOW_SOCKET` and `AETHERFLOW_AGENT_ID` env vars when daemon spawns the opencode server.

Validation: run agent, verify events arrive in daemon memory.

#### Step 2: Session ID capture via plugin

- Add `session.created` event handler that extracts session ID and updates agent state + session registry.
- Remove `captureSessionFromLog` and `captureSpawnSession` JSONL polling goroutines.

Validation: `af sessions` shows correct session ID after agent spawn.

#### Step 3: Refactor `af status` consumers

- `BuildAgentDetail` reads tool calls from event buffer instead of `ParseToolCalls`.
- `buildSpawnDetail` same.
- Session ID comes from agent state (populated by event handler) instead of `ParseSessionID`.

Validation: `af status <agent>` shows tool calls and session ID.

#### Step 4: Refactor `af logs` and TUI

- New daemon RPC method to query/stream events for an agent.
- `af logs <agent>` reads events from daemon instead of tailing JSONL file.
- `af logs -f` subscribes to new events from daemon.
- TUI `LogStreamModel` reads from daemon instead of `readLogLines` on JSONL file.
- Adapt `FormatLogLine` (or write a new formatter) to format plugin event payloads.

Validation: `af logs <agent>` shows tool calls, text, step finishes. `af logs -f` streams in real-time.

#### Step 5: Remove dead JSONL code

- Remove `ParseSessionID`, `ParseToolCalls` from `internal/daemon/jsonl.go`.
- Remove `FormatLogLine` from `internal/daemon/logfmt.go` (or keep if the new formatter wraps it).
- Remove JSONL log file piping from spawn/pool paths.
- Remove `tailFile`/`followFile` from `cmd/af/cmd/logs.go`.
- Remove `readLogLines` from `internal/tui/logstream.go`.

Validation: all tests pass, no references to JSONL log files in hot paths.

#### Step 6: REST API backfill (optional, can defer)

- Add minimal Go HTTP client for `GET /session/:id/message`.
- On daemon restart, backfill event buffer for active sessions from the opencode REST API.

Validation: daemon restart preserves `af status` data for running sessions.

Acceptance target: `af status`, `af logs`, and TUI work with server-first launch. JSONL paths removed.

### Phase B: Harden and extend

- Add stronger concurrency controls (CAS/lease/fencing) only where required by observed contention.
- Add remote target support and stricter trust policy (allowlist/TLS/auth handling).
- Refine session-aware respawn/reclaim behavior and richer stale-state handling.
- Expand WorkSource abstraction and prompt/status decoupling from prog where needed.

Acceptance target: server-first runtime is robust under failure, multi-actor access, and remote targets.

### Future: API orchestration migration (not planned, validated)

Replace `opencode run --attach` process spawning with pure REST API orchestration:

1. `POST /session` → get session ID deterministically
2. `POST /session/:id/prompt_async` → fire prompt, returns immediately
3. Plugin events → observe progress in real-time
4. `opencode attach --session <id>` → human re-attaches when needed

No process spawning, no JSONL piping, no stdout capture. Daemon becomes a pure API orchestrator. Validated in spike 3 but not in scope for current work.

## Acceptance Criteria

### Phase A (SHIPPED)

- [x] Session-oriented CLI commands exist without breaking current pool control commands.
- [x] Global session registry persists and resolves routing metadata with canonical key `{server_ref, session_id}`.
- [x] Registry writes are atomic and safe under concurrent daemon/CLI access.
- [x] Server-first launch is the only runtime path for daemon and spawn flows.
- [x] Attach/detach works on active sessions without ending the underlying session.
- [x] Minimal WorkSource boundary is in place before deeper session lifecycle refactors.

### Phase A.1 (CURRENT)

- [ ] Opencode plugin (`aetherflow-events.ts`) installed and forwarding events to daemon.
- [ ] Daemon receives and buffers plugin events per agent.
- [ ] Session ID captured from `session.created` plugin event (replaces JSONL polling).
- [ ] `af status <agent>` shows tool calls and session ID from event buffer.
- [ ] `af logs <agent>` shows formatted events from event buffer.
- [ ] `af logs -f` streams new events in real-time.
- [ ] TUI log viewer works with event buffer.
- [ ] JSONL log file reads removed from hot paths.

### Phase B

- [ ] Security controls exist for attach targets and credentials (permissions, redaction, validation, URL trust policy).
- [ ] Remote target support.
- [ ] Session-aware respawn/reclaim behavior.

## Failure Modes and Mitigations

### Registry drift

- Mitigation: precedence policy (`live probe > cache > registry`) and heartbeat timestamps; advanced quarantine policy deferred.

### Partial launch failures

- Mitigation: explicit two-phase launch transaction (start client, verify/capture session id, then mark active in registry).

### Concurrency races (daemon reap vs human attach)

- Mitigation: idempotent attach behavior and bounded lock duration in v1; stronger lease/fencing protocol in Phase B.

### Remote server auth/config leakage

- Mitigation: strict redaction in logs, secure file modes (`0700` dirs, `0600` files), env-based secret ingestion, trusted URL policy.

### Plugin failure or crash

- Plugin bugs could affect the opencode server process. Mitigation: keep plugin minimal (dumb pipe), handle all errors with try/catch, never throw from the event hook. If the plugin fails to push an event, the agent continues unaffected.

### Daemon unreachable from plugin

- If the daemon socket is down, the plugin silently drops events. Mitigation: once the daemon comes back, backfill from REST API. The plugin is fire-and-forget — it doesn't retry or buffer.

### Event buffer memory pressure

- Full payloads including tool output are stored in-memory. Worst case: ~150MB for 30 agents with large sessions. Mitigation: ring buffer with bounded size. If memory becomes a problem, first optimization is truncating tool output (consumers only display summaries, full output available via opencode REST API).

### UX ambiguity from command collisions

- Mitigation: keep `af resume` unchanged; introduce `af session attach` namespace.

## Success Metrics

- Attach success rate for known sessions
- Session lookup latency (`af sessions`)
- Registry stale-entry rate
- Crash-recovery success rate in server-first mode
- Regressions in `af status`/`af logs` behavior (must be zero for existing flows)
- Event delivery latency (plugin → daemon buffer → consumer display)

## Dependencies and Prerequisites

- opencode version that supports `serve`, `run --attach`, `attach --session`, plugin system (validated with 1.2.6)
- Daemon config support for server runtime and attach target configuration (shipped in Phase A)
- CLI namespace additions for session commands (shipped in Phase A)

## Risks

- Hidden prog coupling in status/prompt/tooling paths
- Operational complexity around remote URLs/auth and stale registry cleanup
- Plugin running in all opencode sessions globally (mitigated by env var guard — inert when `AETHERFLOW_SOCKET` absent)
- opencode plugin API changes in future versions could break the event pipeline

## Documentation Plan

- Update `README.md` architecture and CLI sections with session-first model.
- Add session registry docs (location, schema, troubleshooting).
- Add release notes for legacy-path deprecation and server-first behavior.
- Document plugin installation and env var configuration.

## References

### Internal

- `docs/brainstorms/2026-02-16-server-first-sessions-brainstorm.md`
- `docs/brainstorms/2026-02-17-plugin-event-pipeline-brainstorm.md`
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

Code shipped in Phase A:

- `internal/daemon/server.go` — managed opencode server lifecycle
- `internal/daemon/server_url.go` — local-only URL validation
- `internal/daemon/spawn_cmd.go` — `EnsureAttachSpawnCmd()`
- `internal/sessions/store.go` — global session registry
- `internal/daemon/work_source.go` — `WorkSource` interface + prog adapter
- `cmd/af/cmd/sessions.go` — `af sessions`, `af session attach`

Observability code to replace in Phase A.1 (JSONL→plugin event pipeline):

- `internal/daemon/jsonl.go` — `ParseSessionID`, `ParseToolCalls` (both dead in attach mode)
- `internal/daemon/logfmt.go` — `FormatLogLine` (needs input from events instead of JSONL)
- `internal/daemon/status.go:239-268` — `BuildAgentDetail` calls `ParseToolCalls` + `ParseSessionID` on log files
- `internal/daemon/status.go:300-326` — `buildSpawnDetail` same pattern
- `internal/daemon/pool.go:556-616` — `captureSessionFromLog` polls log file for session ID
- `internal/daemon/spawn_rpc.go:112-149` — `captureSpawnSession` same pattern
- `cmd/af/cmd/logs.go` — `tailFile` reads/follows JSONL log file directly
- `internal/tui/logstream.go` — `readLogLines` reads JSONL log file for TUI

Spike artifacts:

- `~/.config/opencode/plugins/aetherflow-event-spike.ts` — spike plugin (to be replaced by production plugin)
- `~/.config/aetherflow/spike-events/events.jsonl` — captured event samples

### External

- https://builders.ramp.com/post/why-we-built-our-background-agent
- https://opencode.ai/docs/sdk/
- https://opencode.ai/docs/server/
- https://opencode.ai/docs/plugins/
