# aetherflow

Async runtime for agent work scheduling. Turns intent into reliable, high-quality work across non-deterministic agents by combining a central task system with lightweight messaging and clear state transitions.

## Install

Requires Go 1.21+.

```bash
git clone https://github.com/geobrowser/aetherflow.git
cd aetherflow
go build -o af ./cmd/af
```

## Quick Start

```bash
# Start the daemon in background
af daemon start -d
# Output: daemon started (pid 12345)

# Register an agent
af agent register
# Output: phantom_core

# List agents
af agent list
# ID            STATE  TASK
# phantom_core  idle   -

# Stop the daemon
af daemon stop
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     af daemon start                     │
│                    (daemon process)                     │
│                                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │
│  │   Agents    │  │   Inbox/    │  │   Message   │     │
│  │  Registry   │  │   Outbox    │  │   Router    │     │
│  └─────────────┘  └─────────────┘  └─────────────┘     │
│                                                         │
└──────────────────────┬──────────────────────────────────┘
                       │ Unix socket
                       │ /tmp/aetherd.sock
┌──────────────────────┴──────────────────────────────────┐
│                         af                              │
│                    (CLI client)                         │
│                                                         │
│  af daemon          Check daemon status                 │
│  af daemon start    Start the daemon                    │
│  af agent register  Register and get an ID              │
│  af agent list      List all agents                     │
│  af agent unregister <id>                               │
│  af message send    (coming soon)                       │
│  af message receive (coming soon)                       │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

## CLI Reference

### Daemon

| Command | Description |
|---------|-------------|
| `af daemon` | Check if daemon is running |
| `af daemon start` | Start the daemon (foreground, Ctrl+C to stop) |
| `af daemon start -d` | Start the daemon (background) |
| `af daemon stop` | Stop the background daemon |

### Agents

| Command | Description |
|---------|-------------|
| `af agent register` | Register a new agent, prints assigned ID |
| `af agent list` | List all registered agents |
| `af agent unregister <id>` | Remove an agent |

### Messages (coming soon)

| Command | Description |
|---------|-------------|
| `af message send <to> <summary>` | Send a message |
| `af message receive` | Receive messages from inbox |
| `af message peek` | View inbox without consuming |
| `af message ack <id>` | Acknowledge a message |

## Agent IDs

Agents get hacker-style nicknames instead of UUIDs:

```
phantom_core
ghost_echo
quantum_stream
chrome_packet
neon_daemon
steel_oracle
```

135 adjectives × 135 nouns = 18,225 unique combinations. No numeric suffixes needed.

## Protocol

Communication uses JSON-RPC over Unix socket:

```json
// Request
{"method": "register", "params": null}

// Response
{"success": true, "result": {"agent_id": "phantom_core"}}
```

### Message Envelope (spec)

```json
{
  "id": "01936a2b-7c8d-7e9f-a0b1-c2d3e4f5a6b7",
  "ts": 1234567890000,
  "from": {"type": "agent", "id": "phantom_core"},
  "to": {"type": "overseer"},
  "lane": "task",
  "priority": "P1",
  "type": "status",
  "task_id": "ts-abc123",
  "summary": "Completed initial implementation"
}
```

**Lanes:** `control` (drained first) | `task`

**Priorities:** `P0` (critical) | `P1` (normal) | `P2` (low)

**Types:** `assign` | `ack` | `done` | `abandoned` | `status` | `question` | `blocker` | `review_ready` | `review_feedback`

## Status

**Implemented:**
- [x] Daemon with Unix socket
- [x] Agent registration with nickname generation
- [x] Agent list/unregister
- [x] Message envelope types
- [x] Protocol types with full test coverage

**Coming soon:**
- [ ] Inbox/outbox message storage
- [ ] Message routing
- [ ] Agent state machine
- [ ] Prog integration for task source of truth

## Goals

- Stable throughput over raw utilization
- High quality output with explicit objective and subjective gates
- Clear control-plane semantics (preemption, rebalancing)
- Low coordination overhead through lean messaging
- Fast, repeatable handoffs across agents and teams

## Non-Goals (v1)

- Fully autonomous code generation without oversight
- Task decomposition handled inside the orchestrator
- Heavy metrics dashboards or optimization engines

## License

MIT
