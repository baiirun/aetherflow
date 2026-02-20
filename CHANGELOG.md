# Changelog

## Unreleased

### Breaking Changes

- **Log files removed.** JSONL log files are no longer created for agent sessions. All observability flows through the plugin event pipeline. If you had tooling that read `.aetherflow/logs/*.jsonl`, migrate to `af logs <agent>` or the `events.list` RPC.
- **Default spawn policy is now `manual`.** Previously defaulted to `auto` (poll prog and auto-schedule). Set `--spawn-policy=auto` explicitly to restore the old behavior.

### Added

- **Server-first runtime.** All agents now connect to a shared opencode server via `opencode run --attach <url>`. The daemon auto-starts and supervises the server on `http://127.0.0.1:4096`.
- **Plugin event pipeline.** A plugin on the opencode server (`aetherflow-events.ts`) streams session events to the daemon in real-time. Replaces JSONL log file polling with structured event delivery over Unix socket RPC.
- **`af spawn`** — spawn one-off agents with a freeform prompt, no daemon or task tracker required.
- **`af sessions`** — list known opencode sessions from the global registry.
- **`af session attach <id>`** — attach interactively to a running opencode session.
- **Session registry.** Global persistent registry at `~/.config/aetherflow/sessions/sessions.json` tracks agent-to-session mappings across daemon restarts.
- **Event buffer.** Session-keyed in-memory ring buffer (10K events/session, 48h idle eviction) powers `af logs`, `af status <agent>`, and the TUI.
- **Backfill on startup.** Daemon fetches existing sessions from the opencode REST API on startup so events aren't lost across daemon restarts.
- **`af install` now includes plugins.** The `aetherflow-events.ts` plugin is bundled in the binary and installed alongside skills and agents.
- **`--server-url` flag** and `server_url` config option for the opencode server target.
- **`--spawn-policy` flag** and `spawn_policy` config option (`manual` | `auto`).

### Changed

- `af logs <agent>` reads from the daemon's event buffer instead of tailing JSONL files.
- `af status <agent>` shows tool calls and session IDs from the event buffer.
- TUI log viewer reads from the event buffer.
- `af install` description updated to reflect skills, agents, and plugins.

### Removed

- JSONL log file creation and reading across all code paths.
- `LogDir` and `LogPath` configuration options.
- `--log-dir` CLI flag from `af spawn`.
