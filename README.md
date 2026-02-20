# aetherflow

An opinionated harness for running autonomous agents. Spawn [opencode](https://github.com/anomalyco/opencode) sessions with a prompt and let them work to completion, or connect to [prog](https://github.com/baiirun/prog) for automatic task scheduling.

```
                  +---------------+           +------------------+
                  |  aetherflow   |  attach   |  opencode server |
  af spawn ------>|  (daemon)     |---------->|  (shared)        |
  af daemon ----->|  af / aetherd |<----------|  plugin events   |
                  +---------------+           +------------------+
                        ^                            |
                        |  poll (auto mode)          |  sessions
                  +-----------+               +------------+
                  |   prog    |               |   agent    |
                  |  task db  |               |  processes |
                  +-----------+               +------------+
```

All agent sessions connect to a shared opencode server (`opencode serve`). The daemon starts and supervises this server automatically. A plugin on the server forwards session events (tool calls, messages, lifecycle) back to the daemon over its Unix socket, providing real-time observability without log files.

[prog](https://github.com/baiirun/prog) is a local task management CLI -- create, prioritize, track tasks, and capture learnings. [opencode](https://github.com/anomalyco/opencode) is an agent runtime that runs LLM sessions with tool access. aetherflow bridges them: spawn agents with a prompt (`af spawn`), or let the daemon auto-schedule from prog (`--spawn-policy=auto`).

## Install

### Homebrew (macOS/Linux)

```bash
brew install baiirun/tap/aetherflow
```

### Go install

Requires Go 1.25+.

```bash
go install github.com/baiirun/aetherflow/cmd/af@latest
```

### Build from source

```bash
git clone https://github.com/baiirun/aetherflow.git
cd aetherflow
go build -o af ./cmd/af
```

### Runtime dependencies

aetherflow requires [prog](https://github.com/baiirun/prog) and [opencode](https://github.com/anomalyco/opencode) on PATH:

```bash
brew install baiirun/tap/prog
brew install opencode    # or: curl -fsSL https://opencode.ai/install | bash
```

## Quick Start

### Spawn an agent (no daemon required)

```bash
# Install agent skills and review definitions
af install

# Spawn a one-off agent with a freeform prompt
af spawn "refactor the auth module to use JWT"

# Or in the background
af spawn "add rate limiting to /api/users" -d
```

The agent works in an isolated git worktree, implements the prompt, and creates a PR (or merges to main in `--solo` mode). No daemon or task tracker required -- the prompt is the spec, the PR is the deliverable.

### Run the daemon (automatic task scheduling)

```bash
# Start the daemon with prog integration
af daemon start --project myapp --spawn-policy auto

# Watch the swarm
af status -w

# Or launch the interactive TUI
af tui
```

In auto mode, the daemon polls prog for ready tasks, spawns opencode agents in isolated git worktrees, and monitors their lifecycle. Each agent follows a structured protocol: orient, implement, verify, review, fix, land.

The daemon auto-starts a managed opencode server on `http://127.0.0.1:4096`. All agents connect to this shared server via `opencode run --attach`. The aetherflow plugin on the server streams session events back to the daemon for real-time observability.

### Attaching to a running agent

Every agent session runs on a shared opencode server. You can attach to any running session to watch it work, send it messages, or intervene — then detach and the agent continues autonomously.

```bash
# List active sessions
af sessions

# Attach to a session interactively
af session attach <session-id>

# Inside the opencode TUI:
#   - Watch the agent work in real-time
#   - Send messages to guide or correct it
#   - Quit the TUI (ctrl-c / q) to detach
```

When you detach, the agent process keeps running — it's a separate process connected to the server. Your attach session is just a view into the same opencode session. This works for both `af spawn` agents and daemon pool agents.

You can also monitor without attaching interactively:

```bash
# Stream an agent's events
af logs <agent-name> -f

# One-shot status with tool calls
af status <agent-name>
```

## How It Works With prog

prog manages tasks. aetherflow consumes them.

```bash
# You (or another agent) create tasks in prog
prog add "Implement user auth endpoint" -p myapp --priority 1
prog add "Add rate limiting middleware" -p myapp --priority 2

# aetherflow polls prog, picks up ready tasks, and spawns agents
af daemon start -p myapp --spawn-policy auto

# Agents work autonomously:
#   - Set up git worktree
#   - Read task context from prog (description, logs, learnings)
#   - Implement, test, review
#   - Push branch + create PR (or merge to main in solo mode)
#   - Log progress back to prog throughout

# You monitor from the TUI or CLI
af status -w
af logs worker-ts-a1b2c3 -f
af tui -p myapp
```

### Task Lifecycle

```
prog ready --> aetherflow picks up --> agent spawned --> work happens
                                                              |
                                          prog log <----------+
                                          prog log <----------+
                                          prog done <---------+
```

1. Tasks are created in prog (`prog add ...`)
2. The daemon polls `prog ready -p <project>` on an interval
3. For each ready task, an opencode session is spawned with a structured prompt
4. The agent works in an isolated git worktree (`.aetherflow/worktrees/<task-id>`)
5. Progress is logged back to prog (`prog log <id> "message"`)
6. On completion, the agent either creates a PR or merges to main

### prog Status Integration

aetherflow uses prog's status system to coordinate the full task lifecycle:

| Status | Who sets it | What it means |
|--------|-------------|---------------|
| `open` | You or planner | Task exists but hasn't been picked up |
| `in_progress` | Daemon (via `prog start`) | An agent is actively working on it |
| `reviewing` | Agent (via `prog review`) | Work is complete, PR created, awaiting merge (normal mode only) |
| `done` | Agent or reconciler | Branch merged, task complete |
| `blocked` | Agent (via `prog block`) | Agent hit a wall -- needs human or re-planning |
| `draft` | Agent (via `prog draft`) | Task needs planning -- agent may set to draft if the task is not defined well-enough |
| `canceled` | You | Task is no longer needed |

The daemon only picks up tasks in `open` status that have no unmet dependencies (`prog ready` handles this). Once claimed, the task moves through `in_progress` -> `reviewing` -> `done` (normal mode) or `in_progress` -> `done` (solo mode).

### Agent Knowledge Loop

Agents use prog's context engine to avoid rediscovering what previous agents already learned:

```bash
# Before starting work, agents read task context
prog show ts-a1b2c3           # Task details, logs, deps
prog context -c auth -p myapp  # Relevant learnings from past agents

# After finishing, agents capture what they learned
prog learn "Rate limiter needs Redis for distributed deploys" -c infra -p myapp
```

This creates a flywheel: each agent session makes the next one smarter. Learnings are categorized by concept and project so agents can query for relevant knowledge before starting work.

## Agent Protocol

Every worker agent follows a fixed state machine. The daemon renders a role prompt (embedded in the binary or overridden from disk) and passes it to `opencode run` as the first message. The agent then works autonomously.

```
orient -> feedback loop -> implement -> verify -> review -> fix -> land
```

### orient

Read the task, understand what to build, fill knowledge gaps.

The agent starts by reading the task from prog (`prog show <task-id>`), which includes the description (why/context) and definition of done (what done looks like). It checks for relevant learnings from past agents via `prog context`, reads any handoff notes left by a previous agent that crashed or yielded, and sets up a git worktree at `.aetherflow/worktrees/<task-id>`.

If the task mentions unfamiliar technologies, the agent researches them before writing any code. The research checklist is: project learnings (prog context), project docs, existing codebase patterns, Context7 docs, and web fetch. The gate is: "What exactly am I building, where does it go, and how will I verify it works?" If the agent can't answer all three, it either researches more or yields.

### feedback loop

Establish verification before writing any code.

If the definition of done includes a verification command, the agent runs it now to see the current state (it should fail -- that's the red-to-green signal). If not, the agent creates one: a test, a curl command, a smoke test. The point is to have a fast feedback loop before implementation starts. Tests use the project's test framework and go where tests live in the project.

### implement

Write the code. Run the feedback loop frequently -- after every meaningful change, not just at the end. Fast inner loop: edit -> verify -> adjust.

Agents checkpoint aggressively. Context windows are finite, and if the session compacts, the next continuation only knows what's in git and prog. Agents commit after every logical unit of work and log progress to prog (`prog log <id> "..."`). The bar is: if you lost all memory right now, could you reconstruct where you are from git log + prog logs + file state?

### verify

Full verification pass: the DoD's verification command, full test suite, lint, build.

The agent checks that artifacts match code -- changed behavior means updated help text, added features mean current docs, new code means tests written. Failures in code the agent changed go to the fix cycle. Pre-existing failures that exist on main get filed as new tasks (`prog add`) and are not the agent's problem.

### review

The agent loads the `review-auto` skill, which spawns parallel review subagents on the diff. Each reviewer gets a fresh context (no sunk cost in the implementation) and returns prioritized findings. See [Bundled Skills](#bundled-skills) for details on which reviewers run.

### fix

Address all review findings. The agent deduplicates across reviewers (multiple reviewers often flag the same issue, keeping the highest severity), discards failed reviews, and fixes every P1, P2, and P3 before landing. After fixes, the agent returns to verify. If there are no actionable findings, proceed to land.

### land

Final verification, then ship, then compound knowledge.

In **normal mode**: push the branch, create a PR, clean up the worktree, call `prog review <task-id>`. The daemon's reconciler will detect when the branch is merged to main and automatically call `prog done`.

In **solo mode**: pull latest main, merge the branch with `--no-ff`, push main, clean up branch and worktree, call `prog done <task-id>`. If merge conflicts can't be resolved cleanly, the agent aborts and yields with `prog block`.

After landing, the agent loads the `compound-auto` skill to capture solution documentation, update the feature matrix, log learnings, and write a handoff summary.

## Stuck Detection

Agents monitor their own progress. They're stuck if:

- Same approach tried 3 times with similar edits and the same test keeps failing
- In the fix cycle more than 5 times without the review getting cleaner
- Can't figure out how to verify the DoD
- The task description doesn't match the codebase (references code/APIs that don't exist)
- Unsure whether they're building the right thing

When stuck:

1. **Log everything tried and why it didn't work**: `prog log <id> "Tried X, Y, Z -- all failed because..."`
2. **Yield the task**: `prog block <id> "<reason>"` -- the daemon will respawn a fresh agent with the notes
3. **Stop.** Don't keep thrashing.

When the task itself is the problem (DoD is really multiple tasks, or needs re-planning):

1. **Log the scope issue**: `prog log <id> "Scope issue: <what's wrong>"`
2. **Send it back to draft**: `prog draft <id>` -- a planner can re-break it

The handoff protocol ensures that when a new agent picks up a yielded task, it inherits everything the previous agent learned -- including what didn't work. This is persisted in prog logs (`prog show <id>` includes all logs), not in the git worktree, so it survives agent crashes.

## Handoff Protocol

Used at: session end, blocked/escalation, task yield, task completion.

Agents write handoff summaries to the task log (not the task description, which is the original specification):

```bash
prog log <task-id> "Handoff: <full summary>"
```

Handoffs include:
- What was done
- What is currently being worked on
- Which files are being modified
- What needs to be done next
- What was tried and didn't work, and why
- Key constraints or decisions that should persist

The task description (`prog desc`) is the original specification. Overwriting it destroys context that future agents need. The log is append-only and the next agent reads it via `prog show`.

## Two Modes

### Normal mode (default)

Agents push a branch (`af/<task-id>`) and create a PR. The agent calls `prog review` to signal work is complete. The daemon's reconciler runs on a timer (`--reconcile-interval`, default 30s), fetches main from origin, and checks if the branch is an ancestor of main. When it is (or the branch no longer exists), the reconciler calls `prog done` to close the task.

```
agent finishes -> prog review -> PR created -> someone merges PR
                                                       |
                        reconciler detects merge <------+
                        prog done (auto)
```

### Solo mode (`--solo`)

Agents merge to main directly and call `prog done` themselves. No PR, no reconciler. Use for single-agent workflows or when you want autonomous end-to-end delivery.

```
agent finishes -> merge to main -> prog done
```

If the merge has conflicts the agent can't resolve, it aborts and yields with `prog block`.

## Architecture

Two persistent processes: the aetherflow daemon and a shared opencode server. Everything else (agents, tasks, learnings) is ephemeral or lives in prog.

```
                    daemon                         opencode server
                    (process supervisor)           (shared, managed)
                     |                              |
  af spawn -------->  spawn registry                plugin events
  prog ready ------>  poll -> pool -> spawn ------> agent sessions
                     reap -> respawn           <--- session.event RPC
                     event buffer              <--- tool calls, lifecycle
                     reconcile (reviewing->done)
                     reclaim (orphaned tasks)
```

### Server-First Model

All agents connect to a shared opencode server via `opencode run --attach <url>`. The daemon starts this server automatically on startup and supervises it (restarting if it crashes). The server URL defaults to `http://127.0.0.1:4096` and is configurable via `--server-url` or `server_url` in the config file.

The `AETHERFLOW_SOCKET` env var is set on the server process so the aetherflow plugin knows where to send events. Each agent process gets `AETHERFLOW_AGENT_ID` set to its unique name for session correlation.

### Plugin Event Pipeline

Observability flows through a plugin on the opencode server, not through log files. The aetherflow plugin (`~/.config/opencode/plugins/aetherflow-events.ts`) intercepts session lifecycle events and forwards them to the daemon over its Unix socket via the `session.event` RPC.

**Event buffer** (`eventbuf.go`) -- a session-keyed ring buffer storing up to 10K events per session. Idle sessions are evicted after 2 hours. The buffer is the single source of truth for `af logs`, `af status <agent>`, and the TUI.

**Session claiming** -- when a `session.created` event arrives, the daemon matches the `AETHERFLOW_AGENT_ID` from the event to an unclaimed pool agent or spawn registry entry. This correlates the opencode session ID to the aetherflow agent, enabling event routing.

**Backfill** -- on daemon startup, existing sessions are fetched from the opencode server's REST API (`/session`) and pushed into the event buffer. This covers agents that started before the daemon (re)started.

### Session Registry

The global session registry tracks the mapping between aetherflow agents and opencode sessions. It persists to disk so session metadata survives daemon restarts.

**Location**: `~/.config/aetherflow/sessions/sessions.json` (override with `session_dir` in config)

**Schema** (v1):

```json
{
  "schema_version": 1,
  "records": [
    {
      "server_ref": "http://127.0.0.1:4096",
      "session_id": "abc123",
      "directory": "/path/to/project",
      "project": "myapp",
      "origin_type": "spawn",
      "work_ref": "task-id-or-prompt-hash",
      "agent_id": "worker-ts-a1b2c3",
      "status": "active",
      "created_at": "2026-02-19T12:00:00Z",
      "last_seen_at": "2026-02-19T12:05:00Z",
      "updated_at": "2026-02-19T12:05:00Z"
    }
  ]
}
```

**Fields**:

| Field | Description |
|-------|-------------|
| `server_ref` | Opencode server URL this session belongs to |
| `session_id` | Opencode session ID (from `session.created` event) |
| `origin_type` | How the session was created: `pool` (auto-scheduled), `spawn` (af spawn), `manual` |
| `agent_id` | Aetherflow agent name (e.g., `worker-ts-a1b2c3`) |
| `work_ref` | Task ID (pool/spawn) or prompt reference |
| `status` | `active`, `idle`, `terminated`, `stale` |

**Concurrency**: The registry uses `flock(2)` file locking for safe concurrent access from multiple daemon processes. Writes use atomic rename (write to temp file, rename into place) to prevent corruption.

**Troubleshooting**:

- **Stale entries**: If `af sessions` shows sessions that no longer exist on the server, they'll be marked `stale` on the next status check. This is harmless -- stale entries are ignored by the daemon.
- **Corrupt registry**: Delete `~/.config/aetherflow/sessions/sessions.json` and restart the daemon. It rebuilds from live server state on startup.
- **Permission errors**: The sessions directory uses `0700` and files use `0600`. Check ownership if you see permission denied errors.

### Daemon Internals

The daemon runs several concurrent loops. In `--spawn-policy=manual` (the default), auto task lifecycle loops are disabled (poll/reclaim/reconcile). Manual mode handles `af spawn` agents and their observability.

**Poller** (auto mode only) -- calls `prog ready -p <project>` on an interval to discover unblocked tasks. Returns a list of task IDs and titles. The poller runs in its own goroutine and sends batches to the pool via a channel.

**Pool** (auto mode only) -- manages a fixed number of agent slots (`--pool-size`, default 3). When a batch of ready tasks arrives from the poller, the pool assigns them to free slots. Each slot runs one opencode session. The pool tracks agents by task ID, not by process, so it knows which task each agent is working on.

**Spawn registry** -- tracks agents spawned via `af spawn` (outside the pool). Registration is best-effort via the `spawn.register` RPC. Entries transition from running to exited when the agent process dies, and are kept for 1 hour after exit so `af status <agent>` works post-mortem. A periodic sweep checks PID liveness and removes stale entries.

**Spawn sequence**: For each task, the pool:
1. Fetches task metadata from prog (`prog show --json`) to infer the agent role
2. Renders the role prompt template, replacing `{{task_id}}` and landing instructions
3. Claims the task in prog (`prog start <id>`)
4. Launches an opencode session via `opencode run --attach <server-url>` with the rendered prompt

All fallible prep (1-2) happens before claiming (3) so a failure doesn't orphan the task in `in_progress` with no agent.

**Reaper** -- each spawned agent gets a background goroutine that calls `Wait()` on the process. When the process exits:
- Clean exit (code 0): slot is freed, retry count cleared
- Crash (non-zero): retry counter incremented. If under `--max-retries`, the agent is respawned on the same task (it's already `in_progress` in prog, so `prog start` is skipped). If over the limit, the slot is freed and the task is left in `in_progress` for manual recovery.

**Sweep** -- a safety net that runs every 30s. Checks PID liveness via `kill(pid, 0)` for every tracked agent. If a PID is gone but the reap goroutine is stuck on `Wait()` (observed with `Setsid` session leaders), the sweep force-removes the dead agent from the pool.

**Reclaim** (auto mode only) -- on daemon startup, finds tasks that are `in_progress` in prog but have no running agent. These are orphans from a previous daemon session that crashed. The daemon respawns agents for these tasks (up to pool capacity), using the same respawn path as crash recovery.

**Reconciler** (auto mode, normal landing only) -- periodically checks if `reviewing` tasks have been merged to main. Fetches main from origin (`git fetch origin main`), then for each reviewing task checks `git merge-base --is-ancestor af/<id> main`. If the branch is merged (or already deleted), calls `prog done`. This closes the loop between an agent calling `prog review` and the task reaching its terminal state.

### Agent Isolation

Each agent runs in an isolated git worktree at `.aetherflow/worktrees/<task-id>`. This means:
- Concurrent agents don't clobber each other's files
- Each agent has its own branch (`af/<task-id>`)
- The project root stays on main (agents can read it for reference but all edits go in the worktree)
- Worktrees persist across agent crashes, so a respawned agent can continue where the last one left off

### Process Model

Agents run as child processes of the daemon (pool agents) or the `af spawn` CLI process. Each gets:
- Its own process group (`Setsid: true`) so terminal signals don't propagate
- `AETHERFLOW_AGENT_ID` environment variable for session correlation
- Observability via the plugin event pipeline (no log files)
- stderr passed through to the parent's stderr

The spawn command is configurable (`--spawn-cmd`, default `opencode run --attach http://127.0.0.1:4096 --format json`). If the command doesn't include `--attach`, the daemon automatically appends it with the configured server URL. The rendered prompt is appended as the final argument.

### Name Generator

Each agent gets a memorable name (e.g., `worker-ts-a1b2c3`) generated by the protocol package. Names are unique within a daemon session and released back to the pool when the agent exits.

## TUI

The interactive terminal dashboard (`af tui`) provides a k9s-style interface for monitoring the swarm. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

### Dashboard Screen

The main screen shows:
- **Header**: pool utilization (e.g., `2/3 active`), pool mode (`[draining]`/`[paused]`), project name
- **Agent panes**: one bordered pane per running agent, showing agent name, task ID, task title, uptime, role, and a table of recent tool calls (age, tool name, input summary, duration)
- **Queue**: pending tasks with IDs, priorities, and titles
- **Footer**: keybinding help

Navigate with `j`/`k`, press `enter` to drill into an agent.

### Agent Panel

A two-column detail view for a single agent:

**Left column** (stacked panes):
- **Agent metadata**: name, PID, role, uptime, spawn time, opencode session ID
- **Tool calls**: scrollable table of all tool invocations with timestamps, durations, and input summaries
- **Prog logs**: all `prog log` entries for this task, timestamped

**Right column**:
- **Task info**: full task detail from `prog show` -- title, description, definition of done, status, priority, labels, dependencies

Cycle focus between panes with `tab`/`shift+tab`. Scroll the focused pane with `j`/`k`. Press `l` to enter the full-screen log stream.

### Log Stream

Full-screen event log viewer that reads from the daemon's event buffer. Auto-scrolls to follow new output (`[follow]` indicator). Disable auto-scroll by scrolling up; re-enable with `G` (jump to bottom). Press `g` to jump to top.

### Keybindings

| Screen | Key | Action |
|--------|-----|--------|
| Dashboard | `j`/`k` | Navigate agent panes |
| Dashboard | `enter` | Open agent panel |
| Dashboard | `q`/`esc` | Quit |
| Panel | `tab`/`shift+tab` | Cycle pane focus |
| Panel | `j`/`k` | Scroll focused pane |
| Panel | `l` | Open log stream |
| Panel | `q`/`esc` | Back to dashboard |
| Log Stream | `j`/`k` | Scroll |
| Log Stream | `g` | Jump to top |
| Log Stream | `G` | Jump to bottom + follow |
| Log Stream | `q`/`esc` | Back to panel |

## Bundled Skills, Agents, and Plugins

`af install` copies bundled skills, agent definitions, and plugins into your opencode config directory (`~/.config/opencode/`). These are the tools and infrastructure agents use during their work.

### Plugin: aetherflow-events

Installed to `~/.config/opencode/plugins/aetherflow-events.ts`. This is the event pipeline plugin that streams opencode session events to the aetherflow daemon.

**How it works**: The plugin hooks into opencode's event system and forwards every event (tool calls, messages, session lifecycle) to the daemon's Unix socket via the `session.event` RPC. Events are keyed by opencode session ID; the daemon correlates sessions to agents internally.

**Configuration**: The plugin is controlled entirely by environment variables set automatically by the daemon:

| Variable | Set on | Purpose |
|----------|--------|---------|
| `AETHERFLOW_SOCKET` | opencode server process | Unix socket path for event delivery. When absent, the plugin is completely inert. |
| `AETHERFLOW_AGENT_ID` | agent processes | Agent name for session correlation (used by daemon, not the plugin). |

No manual configuration is needed. `af install` places the file, and the daemon sets the env vars when it starts the opencode server. If the daemon isn't running, the plugin does nothing.

### Skills

**review-auto** -- Autonomous code review using parallel subagent reviewers. Spawned during the `review` phase of the worker protocol. Gathers the diff, launches 6 reviewer agents in parallel, collects and deduplicates their findings, and returns a prioritized list (P1/P2/P3). Each reviewer gets a fresh context with no knowledge of the implementation, so they review the code with fresh eyes.

**compound-auto** -- Knowledge compounding at task completion. Spawned during the `land` phase. Analyzes the work session, extracts and documents non-trivial solutions (to `docs/solutions/`), updates the feature matrix if one exists, logs genuine learnings for future agents, and writes a handoff summary.

### Review Agents

The review skill spawns these 6 agents in parallel:

| Agent | Focus |
|-------|-------|
| **code-reviewer** | Bugs, correctness, logic errors. Strict on existing code modifications, pragmatic on new isolated code. Focuses on clarity, testability, maintainability. |
| **code-simplicity-reviewer** | Unnecessary complexity and simplification opportunities. YAGNI violations, over-abstraction, dead code. |
| **security-sentinel** | Security vulnerabilities. Input validation, authentication, authorization, injection, secrets exposure. |
| **architecture-strategist** | Coupling, cohesion, boundary compliance. Evaluates whether the change fits the system's architecture. |
| **grug-brain-reviewer** | Overengineering and debuggability. Expression simplicity, logging adequacy, API ergonomics, testing philosophy. Speaks in grug voice. |
| **tigerstyle-reviewer** | Safety, assertion density, explicit limits, control flow. NASA Power of Ten rules, assertion-driven design. |

Two additional agents are installed but not part of the default review suite:

| Agent | Focus |
|-------|-------|
| **performance-oracle** | Algorithmic complexity, cache efficiency, scalability, hot paths. |
| **agent-native-reviewer** | Ensures features are agent-native -- any user action has an agent equivalent with full context parity. |

## Flow Control

The pool has three modes that control scheduling behavior:

| Command | Mode | Scheduling | Crash Respawns | Use When |
|---------|------|------------|----------------|----------|
| (default) | `active` | Yes | Yes | Normal operation |
| `af drain` | `draining` | No | Yes | Finishing current work, no new tasks |
| `af pause` | `paused` | No | No | Full stop -- existing agents run but won't be restarted |
| `af resume` | `active` | Yes | Yes | Return to normal after drain/pause |

Drain allows crash respawns because those tasks are already claimed in prog -- leaving them without an agent would orphan them. Pause stops everything, including respawns.

Tasks that arrive during drain or pause are not lost -- they stay in the prog queue and will be picked up on the next poll cycle after `af resume`.

## Configuration

Create `.aetherflow.yaml` in the project directory:

```yaml
project: myapp
# poll_interval: 10s
# pool_size: 3
# spawn_cmd: opencode run --attach http://127.0.0.1:4096 --format json
# server_url: http://127.0.0.1:4096
# spawn_policy: manual        # manual | auto (auto = poll prog and auto-schedule)
# max_retries: 3
# solo: false
# reconcile_interval: 30s

# Config-file-only settings (no CLI flag):
# prompt_dir: ""              # Override embedded prompts with files from this directory
```

CLI flags override config file values. Config file overrides defaults.

### Defaults

| Flag | Default | Description |
|------|---------|-------------|
| `--project` | *(required for auto)* | Prog project to watch for tasks |
| `--poll-interval` | `10s` | How often to poll prog for tasks |
| `--pool-size` | `3` | Maximum concurrent agent slots |
| `--spawn-cmd` | `opencode run --attach <server-url> --format json` | Command to launch agent sessions |
| `--server-url` | `http://127.0.0.1:4096` | Opencode server URL for server-first launches |
| `--spawn-policy` | `manual` | `manual` is spawn-only, `auto` polls/schedules from prog |
| `--max-retries` | `3` | Max crash respawns per task |
| `--solo` | `false` | Agents merge to main directly instead of creating PRs (applies to both `af spawn` and `af daemon start`) |
| `--reconcile-interval` | `30s` | How often to check if reviewing tasks are merged |
| `-d` / `--detach` | `false` | Run in background |

Socket paths are derived automatically from the project name. Each project gets an isolated socket so multiple daemons can run side-by-side. Custom socket paths are not user-configurable.

`--project` is required when `--spawn-policy=auto`, and optional when `--spawn-policy=manual`.
Manual mode without a project uses the global default socket path. Starting a second daemon on the same socket fails fast.

## CLI Reference

### Spawning Agents

| Command | Description |
|---------|-------------|
| `af spawn "<prompt>"` | Spawn a one-off agent with a freeform prompt |
| `af spawn "<prompt>" -d` | Spawn in background (detached) |
| `af spawn "<prompt>" --solo` | Agent merges to main instead of creating a PR |
| `af spawn "<prompt>" --json` | Output spawn metadata as JSON |

### Daemon

| Command | Description |
|---------|-------------|
| `af daemon start` | Start the daemon (manages opencode server, RPC socket, event pipeline) |
| `af daemon start --solo` | All pool agents merge to main instead of creating PRs |
| `af daemon start --spawn-policy auto` | Enable automatic task scheduling from prog |
| `af daemon stop` | Stop the daemon |
| `af daemon` | Quick status check (running/not running) |

### Monitoring

| Command | Description |
|---------|-------------|
| `af status` | Swarm overview -- pool utilization, active agents, queue |
| `af status <agent>` | Agent detail -- task info, uptime, recent tool calls |
| `af status -w` | Watch mode -- continuous refresh |
| `af status --json` | Machine-readable output |
| `af logs <agent> -f` | Tail an agent's event stream (from daemon's event buffer) |
| `af logs <agent> --raw` | Raw events instead of formatted output |
| `af sessions` | List known opencode sessions from the global registry |
| `af sessions --json` | Machine-readable session list |
| `af session attach <id>` | Attach interactively to a session |
| `af tui` | Interactive terminal dashboard (k9s-style) |

### Flow Control

| Command | Description |
|---------|-------------|
| `af drain` | Stop scheduling new tasks, let current work finish |
| `af pause` | Freeze pool -- no scheduling or respawns |
| `af resume` | Resume normal scheduling |

### Setup

| Command | Description |
|---------|-------------|
| `af install` | Install bundled skills, agents, and plugins to opencode config |
| `af install --dry-run` | Preview what would be installed |
| `af install --check` | Exit 0 if up-to-date, 1 if install needed |
| `af install --json` | Structured JSON output for automation |

## Roadmap

**Custom task sources.** The daemon currently requires prog as the task backend. A plugin interface for task sources would let you swap in Linear, GitHub Issues, Jira, or a simple JSON file -- anything that can answer "what's ready?" and "mark this as started."

**External triggers.** Spawn agents from Slack, Linear, Discord, or any webhook. A lightweight API layer that accepts a prompt and queues it into the pool.

**Remote sandboxes.** Run agents in isolated cloud environments instead of local processes. [Sprites](https://sprites.dev) and similar sandboxing runtimes would let you scale beyond your machine and provide stronger isolation between concurrent agents.

## License

MIT
