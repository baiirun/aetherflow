# Server-First Sessions

**Date:** 2026-02-16
**Status:** Ready to plan

## What We're Building

A server-first architecture where the daemon manages opencode servers, and agents and humans are just clients that attach and detach from sessions. This replaces the current model where agents are spawned as standalone `opencode run` processes that can't be interacted with.

The core user-facing feature: `af resume <session-id>` opens an interactive TUI connected to an agent's session. You give feedback, make changes, then detach. The agent (and the session) keep running.

## Why This Approach

### The current model is a dead end for interaction

Today the daemon spawns `opencode run --format json "prompt"` as a child process. The process writes JSONL to a log file. There's no way to send messages to a running agent, attach to its session, or give feedback. When the agent finishes, the session is effectively dead — you can resume it with `--session <id>`, but that requires killing and restarting the process.

### opencode is server-first

opencode is architected as a server with clients on top:
- `opencode serve --port <N>` — headless server
- `opencode run --attach <url>` — autonomous client (agent)
- `opencode attach <url> --session <id>` — interactive TUI client
- Multiple clients can connect to the same server concurrently
- Sessions persist in opencode's SQLite database, survive process exits
- Messages can be injected into a running session from another client

We spiked this and confirmed: you can start an autonomous agent run on a server, then attach interactively to the same session, send messages, and detach — all without killing anything.

### Ramp's Inspect validates this architecture

Ramp built their background coding agent (Inspect) on this exact model. From their writeup (builders.ramp.com):
- Sessions run in sandboxed VMs with opencode as the agent runtime
- Users interact via Slack, web, Chrome extension, VS Code — all synced to the same session
- Multiplayer: multiple people can work in one session together
- Follow-up messages are queued during active execution
- When the agent finishes, sessions are snapshotted and restored on follow-up
- ~30% of their merged PRs come from Inspect

The key insight: **the server is the persistent thing, not the agent process.** Agents and humans are ephemeral clients.

## Key Decisions

### The daemon manages opencode servers, not processes

Instead of spawning `opencode run --format json "prompt"` as a child process, the daemon:
1. Starts an `opencode serve --port <N>` server (one per project, or shared)
2. Runs agents via `opencode run --attach <url> --format json "prompt"`
3. Sessions live in the server, not in the agent process
4. `af resume` is just `opencode attach <url> --session <id>`

The daemon's role shifts from "process supervisor that captures JSONL" to "server manager that routes clients to sessions."

### Sessions are shared ownership: opencode stores history, aetherflow stores routing index

opencode persists session internals in its SQLite DB (`~/.local/share/opencode/opencode.db`) — message history, titles, timestamps, and project directory. We should keep using that as the source of conversational truth.

Aetherflow still needs its own global registry for discovery and routing (especially for remote servers), because `opencode session list` is local-only and doesn't enumerate sessions on arbitrary remote servers.

Query methods available:
- `opencode session list --format json` — list sessions (id, title, timestamps, project, directory)
- `opencode db "<SQL>" --format json` — direct SQLite access for richer queries
- `opencode export <sessionID>` — full session dump including messages

### `af resume` doesn't kill the agent

In the current (pre-server) model, resuming a session requires killing the agent and restarting with `--session`. In the server model, you just attach as another client. The agent keeps running. You can observe, send messages, or take over.

If you want the agent to stop, that's a separate action (`af kill <agent>` or similar). Resume and kill are orthogonal.

### Session registry lives in `~/.config/aetherflow/sessions/`

Global, not per-project. A small aetherflow-owned DB/index enriches opencode session data with routing + swarm context:
- Which task (if any) spawned this session
- Which agent was driving it
- Which project it belongs to
- Which server URL owns it (`http://localhost:...` or remote URL)
- Origin type (pool, spawn, manual)
- Prompt snippet
- Last-seen timestamp / state hints

This is intentionally metadata-only. Session content/history stays in opencode.

### Decoupled from prog

Sessions are identified by session ID, not task ID. This works for pool agents (with prog tasks), spawned agents (no task), and future task sources (Linear, GitHub Issues, etc.). The session is the universal handle.

## Architecture Sketch

```
                    ┌─────────────────────────────┐
                    │     opencode serve :4567     │
                    │                              │
                    │   ┌────────┐  ┌────────┐    │
    af spawn ──────►│   │ ses_A  │  │ ses_B  │    │◄──── af resume ses_A
    (agent run)     │   │(agent) │  │(agent) │    │      (interactive attach)
                    │   └────────┘  └────────┘    │
                    │                              │
                    │   Sessions persist in SQLite │
                    └──────────────┬───────────────┘
                                   │
                    ┌──────────────┴───────────────┐
                    │      aetherflow daemon        │
                    │                              │
                    │  - Manages the server         │
                    │  - Spawns agent clients        │
                    │  - Tracks session→task mapping │
                    │  - Enriches af status/sessions │
                    └──────────────────────────────┘
```

## Open Questions

### Server topology: one server per project or one global server?

opencode servers are scoped to a directory (project). The daemon currently runs per-project too (separate socket per project). One server per project seems natural, but a global server could simplify `af sessions` across projects.

Leaning: one server per daemon instance (follows existing per-project model). `af sessions` can aggregate across servers.

### How does the daemon capture JSONL output?

Resolved by spike: `opencode run --attach <url> --format json` still writes JSONL events to stdout. The daemon can continue redirecting stdout to `.aetherflow/logs/<id>.jsonl` exactly like today.

So existing observability and parsing paths (`af logs`, tool-call parsing, `ParseSessionID`) remain viable in the server-first model.

Open sub-question: if humans also attach interactively, should daemon-visible logs include only agent-run events, or also human-injected prompts/events from the same session?

### What happens to the spawn command config?

`--spawn-cmd` is currently `opencode run --format json`. It would become something like `opencode run --attach <url> --format json`. The `<url>` is dynamic (depends on the server port). This might mean the daemon constructs the command rather than it being a flat config string.

### How does crash recovery work?

Today: agent process exits → `reap()` fires → `respawn()` starts a new process. With the server model: agent client exits → server detects disconnection → daemon needs to notice and spawn a new client on the same session (`opencode run --attach <url> --session <id>`).

The session persists regardless. The question is just how the daemon detects "the agent client disconnected" and decides to reconnect.

### Follow-up message queuing vs immediate injection

Ramp chose to queue follow-up messages during active execution. Our spike showed that opencode injects messages immediately (the agent sees them inline). Need to decide: should `af resume` messages interrupt the agent mid-thought, or queue for after the current step?

Leaning: start with immediate injection (it's what opencode does by default). Queue if it causes problems.

## What This Unlocks

- **`af resume <session-id>`** — attach to any session interactively
- **`af sessions`** — list all sessions with aetherflow context
- **Feedback without kill** — send messages to running agents
- **Multiplayer** — multiple humans in the same session
- **Remote agents** — the server URL can be a cloud VM, not just localhost
- **Web UI** — opencode has a web interface; sessions on a server are automatically web-accessible
- **Decoupled from prog** — sessions are the universal handle, task source is pluggable

## Out of Scope (for now)

- Remote sandbox hosting (Modal, Sprites, etc.) — future work per roadmap
- Multiplayer auth/attribution — Ramp does this, we don't need it yet
- Chrome extension / Slack integration — clients for later
- Session snapshotting/restore — opencode handles this natively
