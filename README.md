# aetherflow

Async runtime for agent work scheduling. A process supervisor that watches [prog](https://github.com/geobrowser/prog) for ready tasks and spawns [opencode](https://github.com/anomalyco/opencode) sessions to work on them.

## Install

Requires Go 1.25+.

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

# Check status
af daemon

# Stop
af daemon stop
```

## Configuration

The daemon loads configuration from three sources (highest priority first):

1. CLI flags
2. Config file (`.aetherflow.yaml`)
3. Defaults

### Defaults

| Flag | Default | Description |
|------|---------|-------------|
| `--project` | *(required)* | Prog project to watch for tasks |
| `--socket` | `/tmp/aetherd.sock` | Unix socket path |
| `--poll-interval` | `10s` | How often to poll prog for tasks |
| `--pool-size` | `3` | Maximum concurrent agent slots |
| `--spawn-cmd` | `opencode run` | Command to launch agent sessions |
| `--max-retries` | `3` | Max crash respawns per task |
| `--config` | `.aetherflow.yaml` | Config file path |

### Config File

Create `.aetherflow.yaml` in the project directory:

```yaml
project: myapp
# socket_path: /tmp/aetherd.sock
# poll_interval: 10s
# pool_size: 3
# spawn_cmd: opencode run
# max_retries: 3
```

CLI flags override config file values.

## Architecture

The daemon is the only persistent process. Everything else (agents, tasks, learnings) is ephemeral or lives in prog.

```
prog ready              daemon                    opencode
  (task queue)    ──>   (process supervisor)  ──>  (agent sessions)
                         poll ─> pool ─> spawn
```

The daemon's job:

1. **Poll** — calls `prog ready -p <project>` on an interval
2. **Infer role** — determines planner vs worker from task metadata
3. **Spawn** — launches opencode sessions with `AETHERFLOW_TASK_ID` and `AETHERFLOW_ROLE` env vars
4. **Monitor** — detects crashed agents, respawns on the same task

The daemon doesn't orchestrate or use an LLM for scheduling. Role inference is deterministic: has DoD? worker. No DoD? planner.

## Status

**Implemented:**
- [x] Daemon with Unix socket (status, shutdown RPCs)
- [x] Poll loop (reads ready tasks from prog)
- [x] Configuration (CLI flags, `.aetherflow.yaml`, validation)
- [x] Agent name generation

**In progress:**
- [ ] Role inference (blocked on prog `--json` support)
- [ ] Agent pool (spawn/reap opencode sessions)
- [ ] Crash detection and respawn

**Planned:**
- [ ] OpenCode plugin (system prompt injection, observability hooks)
- [ ] Observability (`af status`, `af kill`, `af drain`)
- [ ] Learning capture and synthesis

## License

MIT
