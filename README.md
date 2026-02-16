# aetherflow

An opinionated harness for running autonomous agents. A process supervisor that watches [prog](https://github.com/baiirun/prog) for ready tasks and spawns [opencode](https://github.com/anomalyco/opencode) sessions to work on them.

```
    +-----------+           +---------------+           +------------+
    |   prog    |           |  aetherflow   |           |  opencode  |
    |           |   poll    |   (daemon)    |   spawn   |            |
    |  task db  |---------->|  af / aetherd |---------->|   agent    |
    |           |           |               |           |  sessions  |
    +-----------+           +---------------+           +------------+
```

[prog](https://github.com/baiirun/prog) is a local task management CLI -- create, prioritize, track tasks, and capture learnings. [opencode](https://github.com/anomalyco/opencode) is an agent runtime that runs LLM sessions with tool access. aetherflow bridges them: it watches prog for unblocked tasks and spawns opencode sessions to do the work.

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

prog manages tasks. aetherflow consumes them.

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

The daemon is the only persistent process. Everything else (agents, tasks, learnings) is ephemeral or lives in prog.

```
prog ready              daemon                    opencode
  (task queue)    -->   (process supervisor)  -->  (agent sessions)
                         poll -> pool -> spawn
                         reap -> respawn
                         reconcile (reviewing -> done)
                         reclaim (orphaned tasks)
```

### Daemon Internals

The daemon runs several concurrent loops:

**Poller** -- calls `prog ready -p <project>` on an interval to discover unblocked tasks. Returns a list of task IDs and titles. The poller runs in its own goroutine and sends batches to the pool via a channel.

**Pool** -- manages a fixed number of agent slots (`--pool-size`, default 3). When a batch of ready tasks arrives from the poller, the pool assigns them to free slots. Each slot runs one opencode session. The pool tracks agents by task ID, not by process, so it knows which task each agent is working on.

**Spawn sequence**: For each task, the pool:
1. Fetches task metadata from prog (`prog show --json`) to infer the agent role
2. Renders the role prompt template, replacing `{{task_id}}` and landing instructions
3. Opens a JSONL log file at `.aetherflow/logs/<task-id>.jsonl`
4. Claims the task in prog (`prog start <id>`)
5. Launches an opencode session with the rendered prompt as the first message

All fallible prep (1-3) happens before claiming (4) so a failure doesn't orphan the task in `in_progress` with no agent.

**Reaper** -- each spawned agent gets a background goroutine that calls `Wait()` on the process. When the process exits:
- Clean exit (code 0): slot is freed, retry count cleared
- Crash (non-zero): retry counter incremented. If under `--max-retries`, the agent is respawned on the same task (it's already `in_progress` in prog, so `prog start` is skipped). If over the limit, the slot is freed and the task is left in `in_progress` for manual recovery.

**Sweep** -- a safety net that runs every 30s. Checks PID liveness via `kill(pid, 0)` for every tracked agent. If a PID is gone but the reap goroutine is stuck on `Wait()` (observed with `Setsid` session leaders), the sweep force-removes the dead agent from the pool.

**Reclaim** -- on daemon startup, finds tasks that are `in_progress` in prog but have no running agent. These are orphans from a previous daemon session that crashed. The daemon respawns agents for these tasks (up to pool capacity), using the same respawn path as crash recovery.

**Reconciler** -- (normal mode only) periodically checks if `reviewing` tasks have been merged to main. Fetches main from origin (`git fetch origin main`), then for each reviewing task checks `git merge-base --is-ancestor af/<id> main`. If the branch is merged (or already deleted), calls `prog done`. This closes the loop between an agent calling `prog review` and the task reaching its terminal state.

### Agent Isolation

Each agent runs in an isolated git worktree at `.aetherflow/worktrees/<task-id>`. This means:
- Concurrent agents don't clobber each other's files
- Each agent has its own branch (`af/<task-id>`)
- The project root stays on main (agents can read it for reference but all edits go in the worktree)
- Worktrees persist across agent crashes, so a respawned agent can continue where the last one left off

### Process Model

Agents run as child processes of the daemon. Each gets:
- Its own process group (`Setsid: true`) so terminal signals don't propagate
- `AETHERFLOW_AGENT_ID` environment variable for plugin identification
- JSONL stdout captured to `.aetherflow/logs/<task-id>.jsonl`
- stderr passed through to the daemon's stderr

The spawn command is configurable (`--spawn-cmd`, default `opencode run --format json`). The rendered prompt is appended as the final argument.

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

Full-screen JSONL log viewer that reads directly from `.aetherflow/logs/<task-id>.jsonl`. Auto-scrolls to follow new output (`[follow]` indicator). Disable auto-scroll by scrolling up; re-enable with `G` (jump to bottom). Press `g` to jump to top.

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

## Bundled Skills and Agents

`af install` copies bundled skills and agent definitions into your opencode config directory (`~/.config/opencode/`). These are the tools agents use during their work.

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
# spawn_cmd: opencode run --format json
# max_retries: 3
# solo: false
# reconcile_interval: 30s

# Config-file-only settings (no CLI flag):
# prompt_dir: ""              # Override embedded prompts with files from this directory
# log_dir: .aetherflow/logs   # Directory for agent JSONL log files
```

CLI flags override config file values. Config file overrides defaults.

### Defaults

| Flag | Default | Description |
|------|---------|-------------|
| `--project` | *(required)* | Prog project to watch for tasks |
| `--poll-interval` | `10s` | How often to poll prog for tasks |
| `--pool-size` | `3` | Maximum concurrent agent slots |
| `--spawn-cmd` | `opencode run --format json` | Command to launch agent sessions |
| `--max-retries` | `3` | Max crash respawns per task |
| `--solo` | `false` | Agents merge to main directly instead of creating PRs |
| `--reconcile-interval` | `30s` | How often to check if reviewing tasks are merged |
| `-d` / `--detach` | `false` | Run in background |

Socket paths are derived automatically from the project name. Each project gets an isolated socket so multiple daemons can run side-by-side.

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
| `af status` | Swarm overview -- pool utilization, active agents, queue |
| `af status <agent>` | Agent detail -- task info, uptime, recent tool calls |
| `af status -w` | Watch mode -- continuous refresh |
| `af status --json` | Machine-readable output |
| `af logs <agent> -f` | Tail an agent's JSONL log |
| `af logs <agent> --raw` | Raw JSONL instead of formatted output |
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
| `af install` | Install bundled skills and agents to opencode config |
| `af install --dry-run` | Preview what would be installed |
| `af install --check` | Exit 0 if up-to-date, 1 if install needed |
| `af install --json` | Structured JSON output for automation |

## License

MIT
