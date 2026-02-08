# TUI Dashboard Brainstorm

**Date:** 2026-02-07
**Epic:** ep-f4e746 — TUI: Interactive daemon monitor
**Status:** Scoped, ready for task creation

## What We're Building

A full-screen terminal dashboard for monitoring the aetherflow daemon, inspired by k9s (top-level list navigation) and btop (dense multi-pane detail views). Replaces the need for separate `af status`, `af logs`, and `af status --watch` commands.

### Screen 1: Dashboard (k9s-style)

The triage screen. Shows everything at a glance, polls daemon every 2s.

- **Header bar**: pool utilization (e.g. "2/3 active"), pool mode (active/draining/paused), project name
- **Agent table**: name, task ID, task title, role, uptime, last activity summary. Rows update in place on each poll cycle.
- **Task queue**: pending tasks from `prog ready` below the agent table
- **Navigation**: j/k to select agent, Enter to drill into Agent Master Panel
- **Global controls**: p (pause/resume), d (drain), ? (help overlay), q (quit)

### Screen 2: Agent Master Panel (btop-style)

The deep-dive screen. Everything about one agent and its task, all visible simultaneously in a multi-pane layout.

**Panes:**
- **Task info**: title, full description, DoD, dependencies, status (from `prog show --json`)
- **Agent meta**: name, PID, role, uptime, spawn time, retry count
- **Prog logs**: scrollable list of all `prog log` entries for the task
- **Tool call stream**: recent tool calls parsed from JSONL log, live-updating on poll

**Navigation**: Tab to cycle focus between panes, j/k to scroll within focused pane, l to expand log stream full-screen, q to go back to dashboard

### Screen 3: Full Log Stream (expand from Agent Panel)

Full-screen JSONL tail for the selected agent. Auto-follows new output. Scrollback buffer for reviewing history. q to return to Agent Master Panel.

## Key Decisions

1. **bubbletea + lipgloss + bubbles** for the TUI framework (charmbracelet ecosystem, well-maintained, Go-native)
2. **Polling, not streaming** — the daemon RPC is request/response (no event stream). TUI polls on interval like `af status --watch` already does.
3. **Log tailing is file-based** — daemon returns a path via `logs.path` RPC, TUI tails the file directly (same as `af logs` today)
4. **No new daemon RPCs needed initially** — all data available via existing `status.full`, `status.agent`, `logs.path`, pool control RPCs, plus `prog show --json` for task metadata
5. **`af tui` as a new subcommand** — doesn't replace existing CLI commands, just adds a new entry point
6. **Testing gates** — each milestone is human-testable before proceeding to the next

## Technical Notes

- Existing `internal/client` package provides Go API for all 7 daemon RPCs
- Agent detail enrichment (prog show, JSONL parsing) already happens server-side in `BuildAgentDetail()`
- The `internal/term` package has ANSI helpers but TUI will use lipgloss for styling instead
- Pool control RPCs (drain/pause/resume) return `PoolModeResult{Mode, Running}` — fire and forget

## Open Questions

- Should the TUI replace `af status --watch` or coexist? (Leaning: coexist, --watch is useful for scripts/pipes)
- Do we want mouse support eventually? (Leaning: no, keyboard-only like k9s)
- Should prog show data be fetched client-side or should we add a richer daemon RPC? (Leaning: client-side initially, add RPC if latency is a problem)
