# aetherflow

Async runtime for agent work scheduling. A process supervisor that watches [prog](https://github.com/baiirun/prog) for ready tasks and spawns [opencode](https://github.com/anomalyco/opencode) sessions to work on them.

```
              ┌─────────┐         ┌───────────┐         ┌──────────┐
              │  prog    │         │ aetherflow │         │ opencode │
              │          │  poll   │  (daemon)  │  spawn  │          │
              │ task db  │──────>  │  af / aetherd│──────> │  agent   │
              │          │         │            │         │ sessions │
              └─────────┘         └───────────┘         └──────────┘
```

[prog](https://github.com/baiirun/prog) is a local task management CLI — create, prioritize, track tasks, and capture learnings. [opencode](https://github.com/anomalyco/opencode) is an agent runtime that runs LLM sessions with tool access. aetherflow bridges them: it watches prog for unblocked tasks and spawns opencode sessions to do the work.

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

```bash
# 1. Install agent skills and review definitions to opencode
af install

# 2. Start the daemon (foreground)
af daemon start --project myapp

# 3. Watch the swarm
af status -w

# 4. Or launch the interactive TUI
af tui
```

The daemon polls prog for ready tasks, spawns opencode agents in isolated git worktrees, and monitors their lifecycle. Each agent follows a structured protocol: orient, implement, verify, review, fix, land.

## How It Works With prog

prog manages tasks. aetherflow consumes them. The workflow looks like this:

```bash
# You (or another agent) create tasks in prog
prog add "Implement user auth endpoint" -p myapp --priority 1
prog add "Add rate limiting middleware" -p myapp --priority 2

# aetherflow polls prog, picks up ready tasks, and spawns agents
af daemon start -p myapp

# Agents work autonomously:
#   - Set up git worktree
#   - Read task context from prog (description, logs, learnings)
#   - Implement, test, review
#   - Push branch + create PR (or merge to main in solo mode)
#   - Log progress back to prog throughout

# You monitor from the TUI or CLI
af status -w
af logs worker-ts-a1b2c3 -f
```

### Task Lifecycle

```
prog ready ──> aetherflow picks up ──> agent spawned ──> work happens
                                                              │
                                          prog log ◄──────────┤
                                          prog log ◄──────────┤
                                          prog done ◄─────────┘
```

1. Tasks are created in prog (`prog add ...`)
2. The daemon polls `prog ready -p <project>` on an interval
3. For each ready task, an opencode session is spawned with a structured prompt
4. The agent works in an isolated git worktree (`.aetherflow/worktrees/<task-id>`)
5. Progress is logged back to prog (`prog log <id> "message"`)
6. On completion, the agent either creates a PR or merges to main

### Agent Knowledge Loop

Agents use prog's context engine to avoid rediscovering what previous agents already learned:

```bash
# Before starting work, agents read task context
prog show ts-a1b2c3           # Task details, logs, deps
prog context -c auth -p myapp  # Relevant learnings from past agents

# After finishing, agents capture what they learned
prog learn "Rate limiter needs Redis for distributed deploys" -c infra -p myapp
```

This creates a flywheel: each agent session makes the next one smarter.

## Configuration

Create `.aetherflow.yaml` in the project directory:

```yaml
project: myapp
# poll_interval: 10s
# pool_size: 3
# spawn_cmd: opencode run --format json
# max_retries: 3
# solo: false
# reconcile_interval: 30s

# Config-file-only settings (no CLI flag):
# prompt_dir: ""              # Override embedded prompts with files from this directory
# log_dir: .aetherflow/logs   # Directory for agent JSONL log files
```

CLI flags override config file values.

### Defaults

| Flag | Default | Description |
|------|---------|-------------|
| `--project` | *(required)* | Prog project to watch for tasks |
| `--poll-interval` | `10s` | How often to poll prog for tasks |
| `--pool-size` | `3` | Maximum concurrent agent slots |
| `--spawn-cmd` | `opencode run --format json` | Command to launch agent sessions |
| `--max-retries` | `3` | Max crash respawns per task |
| `--solo` | `false` | Agents merge to main directly instead of creating PRs |
| `-d` / `--detach` | `false` | Run in background |

Socket paths are derived automatically from the project name. Each project gets an isolated socket so multiple daemons can run side-by-side.

## Two Modes

**Normal mode** (default): Agents push a branch and create a PR. The daemon's reconciler polls main, detects when the branch has been merged, and transitions the task to `done`.

**Solo mode** (`--solo`): Agents merge to main directly and call `prog done` themselves. No PR, no reconciler. Use for single-agent workflows or when you want autonomous end-to-end delivery.

## Architecture

The daemon is the only persistent process. Everything else (agents, tasks, learnings) is ephemeral or lives in prog.

```
prog ready              daemon                    opencode
  (task queue)    ──>   (process supervisor)  ──>  (agent sessions)
                         poll ─> pool ─> spawn
                         reap ─> respawn
                         reconcile (reviewing → done)
```

### Daemon

1. **Poll** — calls `prog ready -p <project>` on an interval to discover unblocked tasks
2. **Render prompt** — reads the role prompt template, replaces `{{task_id}}` and landing instructions, passes the result as the first message to `opencode run`
3. **Spawn** — launches an `opencode run` session in an isolated git worktree
4. **Monitor** — detects crashed agents and respawns them (up to `--max-retries`)
5. **Reclaim** — on startup, finds orphaned in-progress tasks from a previous daemon session
6. **Reconcile** — periodically checks if `reviewing` tasks have been merged to main and marks them `done`

### Agent Lifecycle

```
orient → feedback loop → implement → verify → review → fix → land
```

- **orient**: Read the task, set up a git worktree, fill knowledge gaps from prog context
- **feedback loop**: Establish verification before writing code
- **implement**: Write code with frequent checkpoints (commits + prog logs)
- **verify**: Run tests, lint, build
- **review**: Spawn parallel review subagents on the diff
- **fix**: Address all review findings
- **land**: Final verification, then push/PR (normal) or merge to main (solo)

## CLI Reference

### Daemon

| Command | Description |
|---------|-------------|
| `af daemon start` | Start the daemon |
| `af daemon stop` | Stop the daemon |
| `af daemon` | Quick status check (running/not running) |

### Monitoring

| Command | Description |
|---------|-------------|
| `af status` | Swarm overview — pool utilization, active agents, queue |
| `af status <agent>` | Agent detail — task info, uptime, recent tool calls |
| `af status -w` | Watch mode — continuous refresh |
| `af status --json` | Machine-readable output |
| `af logs <agent> -f` | Tail an agent's JSONL log |
| `af logs <agent> --raw` | Raw JSONL instead of formatted output |
| `af tui` | Interactive terminal dashboard (k9s-style) |

### Flow Control

| Command | Description |
|---------|-------------|
| `af drain` | Stop scheduling new tasks, let current work finish |
| `af pause` | Freeze pool — no scheduling or respawns |
| `af resume` | Resume normal scheduling |

### Setup

| Command | Description |
|---------|-------------|
| `af install` | Install bundled skills and agents to opencode config |
| `af install --dry-run` | Preview what would be installed |
| `af install --check` | Exit 0 if up-to-date, 1 if install needed |
| `af install --json` | Structured JSON output for automation |

## License

MIT
