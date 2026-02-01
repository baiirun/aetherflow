# Aetherflow Spec (Draft)

## Overview
Aetherflow is an async runtime for agent work scheduling. It turns intent into reliable, high-quality work across non-deterministic agents by combining a central task system with lightweight messaging and clear state transitions.

## Goals
- Stable throughput over raw utilization
- High quality output with explicit objective and subjective gates
- Low coordination overhead through lean messaging
- Fast, repeatable handoffs across agents and teams

## Non-goals (for v1)
- Fully autonomous code generation without human oversight
- Task decomposition and planning fully handled by the system
- Heavy metrics dashboards or optimization engines

## Core concepts
- **Prog**: Source of truth for tasks, priorities, dependency graphs, and work assignments.
- **Agent pool**: Fixed set of registered agents; avoid arbitrary spawning.
- **Teams / tiger teams**: Small collaborative units for consensus mode.
- **Inbox/outbox**: Lightweight message queues for agent-to-agent and agent-to-human communication.
- **Librarian**: Background job that curates knowledge from logs and chats.
- **Daemon**: Communication bus for message routing and agent registry.

## Scheduling model

Agents are autonomous and self-serve from prog:

1. Agent checks `prog ready` to see available work
2. Agent claims a task with `prog start <id>`
3. Agent works, logs progress with `prog log`
4. Agent completes with `prog done` or raises blockers

No central overseer delegates work. Prog is the coordination mechanism - it tracks who is working on what, preventing conflicts. Agents pull work when they have capacity.

Backpressure is natural: if all tasks are claimed, new agents wait. If an agent is blocked, the task remains assigned until resolved or abandoned.

## Messaging protocol

Messages are for coordination, not task assignment. Agents communicate peer-to-peer.

### Use cases
- Agent asks human for help (blocker, question)
- Agent notifies team about something (status, heads up)
- Agent requests review from human
- Agents coordinate on shared work ("I'm changing the interface")
- Human sends guidance or answers to agents

### Envelope
- id
- ts
- from (agent/team id)
- to (agent/team id | human | company_chat | librarian)
- lane (control | task)
- priority (P0 | P1 | P2)
- type (question | blocker | status | review_ready | review_feedback | done | abandoned)
- task_id (required for lane=task; forbidden for lane=control)
- summary (1-2 sentences)
- links (optional: prog task, diff, logs)

### Inbox/outbox
- Each agent has an inbox with two lanes: control and task.
- Control lane is drained first (priority messages).
- Agents push to their outbox; router delivers to recipient inboxes.
- Agents poll their inbox at natural breakpoints (after tool calls, between subtasks).
- Push/interrupt is not possible - MCP tools cannot invoke Claude.

### Message routing
- Outbox is source of truth for pending messages
- Router polls outboxes and delivers to recipient inboxes based on `to` field
- Inbox is ephemeral (rebuilt on restart from pending outbox messages)
- Librarian filters for `to=librarian`
- Company chat filters for `to=company_chat`

### Tiger team chat
- Treated as a team inbox/outbox with `to=team:<id>`.
- Agents in a team can message each other to coordinate.
- No designated scribe needed - any agent can communicate externally.

## Task source of truth
- Tasks live in prog with priorities and dependency graphs.
- Agents claim work directly from prog.
- Messaging protocol only includes the task identifier; agent reads full task details from prog at start.
- Prog tracks assignments - prevents two agents from claiming the same task.

## Agent state machine (initial)
This should be tuned in practice; keep heuristics lightweight and adjust with real usage.

### States
idle -> active -> (question | blocked | ready_for_review) -> done
fallback: active -> abandoned (if task is invalidated or re-scoped)

### Transitions + heuristics
- idle -> active: agent claims task from prog (`prog start`)
- active -> question: ambiguity that doesn't block progress; include options + default choice
- active -> blocked: missing dependency, unclear requirement, or external approval needed; include minimal question + proposed next step
- active -> ready_for_review: task checklist complete; tests pass (or explicitly skipped with reason); no open questions; diff scoped to task; required artifacts attached (notes/logs)
- ready_for_review -> done: review passes; changes merged or task closed in prog
- ready_for_review -> active: review requests changes; include specific action list
- any -> abandoned: task invalidated; record reason + link to replacement task if any

## Status snapshot (handoff)
Use a structured `prog log` entry with a `status_snapshot` prefix for clean handoffs.

### Prompting fields
- state (active | blocked | question | ready_for_review)
- progress (2-4 bullets)
- next_steps (1-3 concrete actions)
- open_questions (unresolved decisions)
- tried_and_rejected (what was attempted + why it failed or was skipped)
- artifacts (links to diff, notes, tests, logs)
- risks_or_assumptions (anything fragile or implicit)

## Quality gates (objective + subjective)
- Objective: tests written/run, lint/format checks, project rule checks, docs updates as required.
- Subjective: clarity, robustness, maintainability, consistency with local patterns, testability.
- Define concrete heuristics per project at implementation time.

## System improvement signals (not speed KPIs)
- Derived from prog logs and review outcomes.
- Focus on autonomy: blocker rate, rework rate, repeated mistakes, handoff quality.

## Open questions
- Reliability semantics (at-least-once vs exactly-once, dedupe, replay)
- Ordering guarantees (per sender, per task)
- Message ack and timeout behavior
- Knowledge capture design across logs, librarian, and prog learn


## Runtime daemon interface (v1)
- CLI daemon first for portability, scripting, and easy local deployment.
- MCP adapter is a later transport layer that can wrap the CLI or share the same inbox/outbox storage.
- Keep daemon logic independent of transport so swapping CLI/MCP is low-friction.

### Daemon responsibilities
- Agent registry (track who's alive)
- Message routing (outbox → inbox delivery)
- Liveness detection (heartbeats, lease expiry)

The daemon is a communication bus, not a scheduler. Work assignment happens through prog.

## Agent registration protocol

### Agent ID assignment
- Daemon-assigned on first registration
- Format: hacker-style nicknames (e.g., `ghost_echo`, `quantum_stream`, `chrome_packet`)
- Generated from adjective + noun (135 × 135 = 18,225 combinations)
- Daemon tracks used names and retries on collision
- Agent provides optional human name/labels, daemon returns assigned nickname
- On restart, agent must re-register and gets a new ID (old ID expires)

### Registration flow
```
Agent                           Daemon
  |                               |
  |-- RegistrationRequest ------->|
  |   (name, labels, capacity,    |
  |    heartbeat_interval)        |
  |                               |
  |<-- RegistrationResponse ------|
  |   (agent_id, accepted,        |
  |    lease_expires_at)          |
  |                               |
  |-- Heartbeat (periodic) ------>|
  |   (agent_id, state,           |
  |    queue_depth, current_task) |
  |                               |
  |<-- HeartbeatResponse ---------|
  |   (lease_expires_at,          |
  |    pending_messages)          |
```

### Liveness
- Lease-based with heartbeats (default 30s interval)
- Lease expires if no heartbeat received within 3x interval
- Expired agents are marked stale; their claimed tasks may be released back to prog

### Unregistration
- Explicit unregister releases any claimed tasks back to prog
- Or agent just stops heartbeating and lease expires naturally

## CLI commands

```
aetherflow daemon start [--foreground]
aetherflow daemon stop
aetherflow daemon status

aetherflow agent register [name] [--label X] [--capacity N]
aetherflow agent list
aetherflow agent unregister <agent-id>

aetherflow message send <to> <summary> [--lane control|task] [--priority P0|P1|P2] [--type X] [--task ID]
aetherflow message receive [--lane X] [--wait] [--limit N]
aetherflow message peek [--lane X] [--limit N]
aetherflow message ack <message-id>
```

## Typical agent workflow

```
1. Register with daemon
   $ af agent register

2. Check for available work
   $ prog ready --project myproject

3. Claim a task
   $ prog start ts-abc123

4. Work on task, log progress
   $ prog log ts-abc123 "Implemented feature X"

5. Poll inbox periodically for messages
   $ af message peek

6. If blocked, send message to human
   $ af message send human "Blocked on API credentials" --type blocker --task ts-abc123

7. Complete task
   $ prog done ts-abc123
```
