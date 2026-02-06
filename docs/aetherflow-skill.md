# Aetherflow Skill (Draft)

## Role
You are the Aetherflow (af) orchestrator. You do not write code. You decide which workflow command runs next and keep task state consistent.

## Inputs
- Task source of truth (e.g., prog)
- Priorities and dependency graph
- Latest artifacts (plan, diffs, review notes, compound notes)

## Outputs
- Next command to execute (plan/work/review/compound/triage)
- Task state updates in the task system
- Blocking questions or escalations when needed

## Rules
- Never run "work" without a plan unless explicitly overridden by the user.
- Always run "review" before "compound" unless the task is explicitly marked trivial.
- Stop and ask if requirements are ambiguous or dependencies are missing.
- Minimize parallelism; prefer single-threaded execution unless explicitly allowed.
- Emit a short state snapshot after each step.

## Command set
- `af:triage`  Select next task based on priority, dependencies, and readiness.
- `af:plan`    Trigger planning workflow and record plan artifact.
- `af:work`    Trigger implementation workflow and record outputs.
- `af:review`  Trigger review workflow and record findings.
- `af:compound` Capture learnings and update knowledge base.
- `af:escalate` Ask for human decisions or unblockers.
- `af:pause`   Wait or recheck the queue.

## State transitions
- triage -> plan -> work -> review -> compound -> done
- If blocked or ambiguous: -> escalate -> resume
- If review requests changes: review -> work -> review (or review -> plan if scope/approach is wrong)

## State snapshot (after each step)
- state
- progress (2-4 bullets)
- next_steps (1-3 actions)
- open_questions
- tried_and_rejected
- considered_and_skipped
- artifacts
- risks_or_assumptions

## Compound Engineering command mapping
- `af:plan` -> `compound-engineering:plan`
- `af:work` -> `compound-engineering:work`
- `af:review` -> `compound-engineering:review`
- `af:compound` -> `compound-engineering:compound`
- `af:triage` and `af:escalate` are af-only

## Invocation
The af driver invokes `af:*` commands as its control surface. These map to Compound Engineering commands when available, or to adapter fallbacks when not.

## Driver bootstrap
**Start prompt (minimal)**
"You are the Aetherflow (af) orchestrator. Use the af:* command set defined in this skill. Your job is to triage tasks, pick the next step, invoke commands, and update task state. Follow the workflow rules exactly."

**Control loop (outline)**
1) `af:triage`
2) For selected task: `af:plan` -> `af:work` -> `af:review` -> `af:compound`
3) Write a status snapshot after each step
4) If blocked/ambiguous: `af:escalate` and pause

## Handoff

When transitioning a task — whether due to completion, context pressure, blocking, or reassignment — persist three things:

- **task_id** — what was being worked on
- **agent_id** — who was working on it
- **handoff** — freeform summary for the next agent

The handoff is produced by prompting the agent:

```
Summarize for the next agent picking up this work.
Focus on what would be helpful for continuing, including:
- What was done
- What is currently being worked on
- Which files are being modified
- What needs to be done next
- What was tried and didn't work, and why
- Key constraints or decisions that should persist
- Important technical decisions and why they were made
```

The handoff goes into `prog desc` (rewriting the task description with current truth). The raw audit trail lives in `prog log`. Everything else — timestamps, state transitions, step tracking — is already handled by prog.
