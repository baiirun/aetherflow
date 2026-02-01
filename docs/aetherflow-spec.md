# Aetherflow Spec (Draft)

## Overview
Aetherflow is an async runtime for agent work scheduling. It turns intent into reliable, high-quality work across non-deterministic agents by combining a central task system with lightweight messaging and clear state transitions.

## Goals
- Stable throughput over raw utilization
- High quality output with explicit objective and subjective gates
- Clear control-plane semantics (preemption, rebalancing)
- Low coordination overhead through lean messaging
- Fast, repeatable handoffs across agents and teams

## Non-goals (for v1)
- Fully autonomous code generation without oversight
- Task decomposition and planning fully handled inside the orchestrator
- Heavy metrics dashboards or optimization engines

## Core concepts
- **Prog**: Source of truth for tasks, priorities, and dependency graphs.
- **Overseer**: Schedules work by delegating task IDs to agent/team inboxes.
- **Agent pool**: Fixed set of registered agents; avoid arbitrary spawning.
- **Teams / tiger teams**: Small collaborative units for consensus mode.
- **Inbox/outbox**: Lightweight message queues for control and task messages.
- **Librarian**: Background job that curates knowledge from logs and chats.

## Scheduling model
- Overseer pulls from prog and delegates work to agents/teams.
- Admission control and backpressure are handled at the overseer.
- Keep per-agent queue caps small to avoid thrash.
- Rebalancing is done by overseer when idle or blocked time accumulates.

## Messaging protocol (slim)
Messages contain minimal metadata. All task detail lives in prog.

### Envelope
- id
- ts
- from (agent/team id)
- to (overseer | agent/team id | company_chat | librarian)
- lane (control | task)
- priority (P0 | P1 | P2)
- type (assign | ack | question | blocker | status | review_ready | review_feedback | done | abandoned)
- task_id (required for lane=task; forbidden for lane=control)
- summary (1-2 sentences)
- links (optional: prog task, diff, logs)

### Inbox/outbox
- Agents/teams have two inbox lanes: control and task.
- Control lane is always drained first.
- Outbox can mirror lanes, but default is a single outbox filtered by `to`.
  - Overseer filters for `to=overseer`.
  - Librarian filters for `to=librarian`.
  - Optional company chat filters for `to=company_chat`.
- Overseer handles overload by deferring new assignments, prioritizing P0 control, and rebalancing work.

### Tiger team chat
- Treated as a team inbox/outbox with `to=team:<id>`.
- One designated scribe/driver is responsible for task-linked messages to overseer.

## Task source of truth
- Tasks live in prog with priorities and dependency graphs.
- Overseer reads from prog and delegates to agent/team inboxes.
- Messaging protocol only includes the task identifier; agent/team reads full task details from prog at start.

## Agent state machine (initial)
This should be tuned in practice; keep heuristics lightweight and adjust with real usage.

### States
idle -> queued -> active -> (question | blocked | ready_for_review) -> review -> done
fallback: active -> abandoned (if task is invalidated or re-scoped)

### Transitions + heuristics
- queued -> active: agent accepts task and loads details from prog
- active -> question: ambiguity that doesn't block progress; include options + default choice
- active -> blocked: missing dependency, unclear requirement, or external approval needed; include minimal question + proposed next step
- active -> ready_for_review: task checklist complete; tests pass (or explicitly skipped with reason); no open questions; diff scoped to task; required artifacts attached (notes/logs)
- ready_for_review -> review: overseer/auditor begins review
- review -> done: review passes; changes merged or task closed in prog
- review -> active: review requests changes; include specific action list
- any -> abandoned: task invalidated; record reason + link to replacement task if any

## Status snapshot (handoff)
Use a structured `prog log` entry with a `status_snapshot` prefix so the overseer can parse it.

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
- Control message ack and timeout behavior
- Knowledge capture design across logs, librarian, and prog learn


## Runtime daemon interface (v1)
- CLI daemon first for portability, scripting, and easy local deployment.
- MCP adapter is a later transport layer that can wrap the CLI or share the same inbox/outbox storage.
- Keep daemon logic independent of transport so swapping CLI/MCP is low-friction.

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
- Expired agents are marked stale; pending messages requeued to overseer

### Unregistration
- Explicit unregister sends pending messages back to overseer
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
