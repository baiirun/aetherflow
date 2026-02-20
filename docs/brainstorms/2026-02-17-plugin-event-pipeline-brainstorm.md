# Plugin Event Pipeline for Server-First Observability

**Date:** 2026-02-17
**Status:** Incorporated into plan
**Related:** docs/plans/2026-02-16-feat-introduce-server-first-sessions-global-registry-plan.md (Phase A complete, observability gaps remain)

## What We're Building

An event pipeline from the opencode server to the aetherflow daemon using an opencode plugin. The plugin subscribes to all server events (session lifecycle, tool calls, messages, status) and pushes them to the daemon over its existing Unix socket RPC. The daemon stores events in-memory and exposes them to `af status`, `af logs`, and the TUI — replacing the JSONL log file reads that are dead in server-first (attach) mode.

## Why This Approach

### JSONL is dead in attach mode

Phase A switched the launch path to `opencode run --attach <url>`. In this mode, opencode writes **zero bytes to stdout**. The `--format json` flag has no effect. Every JSONL-based observability path is broken:

- `ParseSessionID` — never finds a session ID
- `ParseToolCalls` — returns empty
- `FormatLogLine` — has no input
- `captureSessionFromLog` — exhausts 40 retries silently
- `af logs` / TUI log stream — read empty files

### Plugin events give us everything and more

The opencode plugin system (`docs/plugins/`) provides in-process event subscriptions. The available events cover every data point we currently extract from JSONL, plus new ones we didn't have:

| Current JSONL data | Plugin event equivalent |
|---|---|
| `sessionID` (first line) | `session.created` (includes `info.id`) |
| `tool_use` events | `message.part.updated` (type=tool), `tool.execute.before/after` |
| `text` events | `message.part.updated` (type=text) |
| `step_finish` (tokens, cost) | `message.updated` (includes token counts, cost) |
| Process alive = busy | `session.status` (busy/idle/retry), `session.idle`, `session.error` |

**New data not available via JSONL:**
- `tool.execute.before` — see tool args before execution starts
- `session.error` — explicit error events (currently inferred from process exit)
- `session.compacted` — know when context was compacted
- `session.idle` — explicit completion signal

### Scales to many agents

SSE would require one persistent connection per opencode server. REST polling would require O(agents) requests per poll interval. The plugin approach inverts this: each agent pushes its own events. Zero polling. One Unix socket connection per event (cheap, local). The daemon is passive.

### Future-proof for the daemonless vision

In the future, agents will export metrics directly to a central observability layer — no daemon in the middle. The plugin already runs inside each agent's opencode server. When the central service exists, we change the push destination from a Unix socket to an HTTP URL. The plugin stays the same. The env var that configures the destination is the swap point.

## Key Decisions

### Plugin is a dumb pipe — forward all events, daemon filters

The plugin forwards every event it receives to the daemon without filtering. The daemon decides what to keep, index, and expose.

**Rationale:**
- Plugin is zero-maintenance. New event types require changes only in the daemon.
- Volume is manageable: a busy agent produces hundreds of events over minutes/hours, each 1-5KB. Over a local Unix socket, this is negligible.
- When the destination swaps to a central service, we want the full stream there too. Filtering at the source means updating every deployed plugin when needs change.

### In-memory event buffer + REST API backfill on restart

Events are stored in a per-agent in-memory ring buffer in the daemon. No persistence layer. Full event payloads including tool output text — no truncation. We measured the worst case at ~150MB for 30 agents with large sessions, which is acceptable for a dev machine. If memory becomes a problem in practice, the first optimization lever is truncating tool output (consumers only display summaries anyway, full output is always available via the opencode REST API).

**On daemon restart:** backfill from the opencode REST API (`GET /session/:id/message`) which returns the full message tree with all tool call parts. This is the same data we'd have persisted ourselves.

**Rationale:**
- The daemon is long-running. In-memory covers the 90% case.
- opencode already persists everything in SQLite. Duplicating persistence is wasted work.
- The REST API backfill validates API client code we'll need for the `POST /session` + `prompt_async` launch path later.
- Store full payloads now, optimize later if needed. Premature truncation would complicate the code for a problem we haven't hit.

### Environment variables configure the plugin

The daemon sets environment variables when spawning the opencode server:

```
AETHERFLOW_SOCKET=/tmp/aetherd-myproject.sock
AETHERFLOW_AGENT_ID=ghost_wolf
```

- `AETHERFLOW_SOCKET` — where to push events. Absent = plugin is inert. Future swap point.
- `AETHERFLOW_AGENT_ID` — which agent this server belongs to. Used to route events to the right buffer.

The plugin checks for `AETHERFLOW_SOCKET` on init. If missing, it returns empty hooks and does nothing. Safe for standalone opencode use.

### Plugin speaks the existing daemon RPC protocol

The daemon already uses JSON-over-Unix-socket RPC (request envelope: `{method, params}`, response: `{success, result, error}`). The plugin uses the same protocol via Node's `net.createConnection`. One connection per event, fire-and-forget (don't block on response for non-critical events).

New RPC method:

```
Method: "session.event"
Params: {
  agent_id: string,        // from AETHERFLOW_AGENT_ID
  event_type: string,      // "session.created", "message.part.updated", etc.
  session_id: string,      // extracted from event payload
  timestamp: number,       // unix millis
  data: object             // raw event properties (forwarded as-is)
}
```

## Architecture

```
opencode server (manages sessions, runs agents)
  │
  ├── aetherflow plugin (subscribes to all events)
  │     │
  │     └── pushes events → daemon Unix socket RPC ("session.event")
  │
  └── REST API (backfill source on daemon restart)
        │
        └── GET /session/:id/message → full message tree

daemon (receives events, stores in-memory)
  │
  ├── per-agent event buffer (ring buffer, bounded)
  ├── session ID capture (from session.created events, replaces captureSessionFromLog)
  ├── session status tracking (from session.status/idle/error events)
  │
  └── consumers
        ├── af status <agent> → reads tool calls from buffer
        ├── af logs <agent>   → reads formatted events from buffer
        ├── af logs -f        → streams new events as they arrive
        └── TUI log viewer    → reads/streams from buffer
```

## Components to Build

### 1. Opencode plugin (`aetherflow-events`)

TypeScript plugin, installed globally at `~/.config/opencode/plugins/aetherflow-events.ts`.

- Reads `AETHERFLOW_SOCKET` and `AETHERFLOW_AGENT_ID` from environment
- If absent, returns empty hooks (inert)
- Subscribes to `event` hook, forwards all events to daemon RPC
- Extracts `session_id` from event payload (location varies by event type)
- Handles socket errors gracefully (log warning, don't crash the agent)

### 2. Daemon event handler + buffer

New package or addition to existing daemon:

- `session.event` RPC handler receives events from the plugin
- Per-agent ring buffer (configurable size, e.g. 1000 events)
- Index by agent ID for fast lookup
- Session ID extraction on `session.created` → updates agent state + session registry
- Status tracking on `session.status`/`session.idle`/`session.error`

### 3. Refactored consumers

Replace JSONL reads with event buffer reads:

- `BuildAgentDetail` → read tool calls from buffer instead of `ParseToolCalls`
- `buildSpawnDetail` → same
- `captureSessionFromLog` → replaced by `session.created` event handler
- `captureSpawnSession` → same
- `af logs` → read from daemon RPC (new method) instead of file tailing
- TUI `readLogLines` → read from daemon RPC instead of file reading

### 4. REST API client (minimal, for backfill)

Lightweight Go HTTP client for opencode REST API:

- `GET /session/:id/message` — fetch full message tree for backfill
- Used on daemon restart to populate buffer for active sessions
- Reusable for future `POST /session` + `prompt_async` work

### 5. Environment variable injection

Daemon sets `AETHERFLOW_SOCKET` and `AETHERFLOW_AGENT_ID` when spawning the opencode server process. Already controls the environment in the server management code.

## Open Questions

- **Plugin installation**: Global install at `~/.config/opencode/plugins/` means it affects all opencode usage, not just aetherflow-managed servers. The env var guard makes it inert outside aetherflow, but should we also support project-local install (`.opencode/plugins/`)?
- **Event batching**: Should the plugin batch events and send periodically, or send each event individually? Individual is simpler. Batching reduces socket connections but adds latency and buffering complexity.
- **Ring buffer eviction**: When the buffer fills, oldest events are dropped. Should we signal this to consumers (e.g., "500 older events not shown, use opencode for full history")?
- **Multiple agents per server**: If multiple agents attach to the same opencode server, the plugin receives events for all of them on a single `event` hook. We need to route events to the correct agent ID using the session ID.

## Next Steps

Implementation phases defined in plan — see Phase A.1 in `docs/plans/2026-02-16-feat-introduce-server-first-sessions-global-registry-plan.md`.
