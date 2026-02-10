# aetherflow

Async runtime for agent work scheduling. A process supervisor that watches [prog](https://github.com/geobrowser/prog) for ready tasks and spawns [opencode](https://github.com/anomalyco/opencode) sessions to work on them.

[prog](https://github.com/geobrowser/prog) is a local task management CLI (create, prioritize, track tasks). [opencode](https://github.com/anomalyco/opencode) is an agent runtime that runs LLM sessions with tool access. aetherflow bridges them — it watches prog for work and spawns opencode sessions to do it.

## Install

Requires Go 1.25+. Runtime dependencies: [prog](https://github.com/geobrowser/prog) and [opencode](https://github.com/anomalyco/opencode) must be installed and on PATH.

```bash
git clone https://github.com/geobrowser/aetherflow.git
cd aetherflow
go build -o af ./cmd/af
```

## Quick Start

```bash
# Start the daemon (foreground)
af daemon start --project myapp

# Start in background
af daemon start --project myapp -d

# Watch the swarm
af status -w

# Drill into an agent
af status <agent-name>

# Tail an agent's log
af logs <agent-name> -f

# Flow control
af drain    # stop scheduling new tasks, let current work finish
af pause    # freeze — no scheduling or respawns
af resume   # back to normal

# Stop
af daemon stop
```

## Configuration

The daemon loads configuration from three sources (highest priority first):

1. CLI flags
2. Config file (`.aetherflow.yaml`)
3. Defaults

### Config File

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
| `--config` | *(none)* | Config file path (defaults to `.aetherflow.yaml` in project dir when unset) |
| `-d` / `--detach` | `false` | Run in background |

Socket paths are derived automatically from the project name. Each project gets an isolated socket so multiple daemons can run side-by-side.

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
2. **Infer role** — currently all tasks get the worker role. Planner role is scaffolded but role inference is not yet active
3. **Render prompt** — reads the role prompt template (embedded in the binary, or from a custom directory via `prompt_dir` in the config file), replaces `{{task_id}}` and landing instructions, passes the result as the first message to `opencode run`
4. **Spawn** — launches an `opencode run` session whose prompt instructs the agent to work in an isolated git worktree (`.aetherflow/worktrees/<task-id>`)
5. **Monitor** — detects crashed agents and respawns them on the same task (up to `--max-retries`)
6. **Reclaim** — on startup, finds orphaned in-progress tasks from a previous daemon session and respawns agents for them
7. **Reconcile** — periodically checks if `reviewing` tasks have been merged to main and marks them `done` via `prog done` (skipped in solo mode)

### Two Modes

**Normal mode** (default): Agents push their branch and create a PR. The daemon's reconciler polls main, detects when the branch has been merged, and transitions the task from `reviewing` to `done`.

**Solo mode** (`--solo`): Agents merge their branch to main directly and call `prog done` themselves. No PR, no reconciler. Use when running a single agent or when you want autonomous end-to-end delivery.

### Agent Lifecycle

Each agent follows a structured protocol:

```
orient → feedback loop → implement → verify → review → fix → land
```

- **orient**: Read the task, set up a git worktree, fill knowledge gaps
- **feedback loop**: Establish verification before writing code
- **implement**: Write code with frequent checkpoints (commits + prog logs)
- **verify**: Run tests, lint, build
- **review**: Spawn parallel review subagents on the diff
- **fix**: Address all review findings
- **land**: Final verification, then push/PR (normal) or merge to main (solo)

Agents work in isolated git worktrees so multiple agents can run concurrently without clobbering each other's files.

## CLI

| Command | Description |
|---------|-------------|
| `af daemon start` | Start the daemon |
| `af daemon stop` | Stop the daemon |
| `af daemon` | Quick status check (running/not running) |
| `af status` | Swarm overview — pool utilization, active agents, queue |
| `af status <agent>` | Agent detail — task info, uptime, recent tool calls |
| `af status -w` | Watch mode — continuous refresh |
| `af status --json` | Machine-readable output |
| `af logs <agent> -f` | Tail an agent's JSONL log |
| `af logs <agent> --raw` | Raw JSONL instead of formatted output |
| `af drain` | Stop scheduling new tasks, let current work finish |
| `af pause` | Freeze pool — no scheduling or respawns |
| `af resume` | Resume normal scheduling |
| `af tui` | Interactive terminal dashboard (k9s-style) |

## License

MIT
