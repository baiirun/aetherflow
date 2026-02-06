# Compaction Handoff Hook

**Date:** 2026-02-06
**Task:** ts-b54507
**Status:** Ready to plan

## What We're Building

An opencode plugin that hooks into `experimental.session.compacting` to:

1. **Replace the compaction prompt** with aetherflow's handoff format so the compacted summary is structured for agent continuation
2. **Inject task context** (task ID, role) so they survive compaction
3. **Instruct the agent to write progress to prog** as part of the compaction summary step — so both the agent's future self AND external observers have the current state

## Why This Approach

From our live agent test runs (ts-39f50e), we observed:
- Opencode auto-compacts even in `run` mode (`compaction.auto: true` by default)
- The agent doesn't die on context limit — it compacts and continues
- But the default compaction prompt is generic, not task-aware
- Without task-specific context in the compaction, the agent loses track of what it was doing, what state it's in (orient/implement/verify/etc.), and what didn't work

The compaction hook is the primary mechanism for context survival, not respawning. Aggressive checkpointing (git commits, prog logs) is the fallback.

## Key Decisions

### One prompt for both compaction and prog update
The compaction prompt tells the agent to both summarize for its own continuation AND write the summary to prog. There will be duplication — the compacted agent gets the summary in its context, and prog gets a copy. This is intentional: the daemon, humans, and future respawned agents all need visibility into the current state.

### Separate plugin file
The compaction hook lives in `.opencode/plugins/compaction-handoff.ts`, separate from the activity logger. Each plugin does one thing. Both registered in `opencode.json`.

### Task ID from conversation, not env vars
The hook parses the task ID from the first message in the conversation (the rendered worker prompt contains `Task: ts-xxxxx`). No dependency on AETHERFLOW_TASK_ID env var. This works today without the env var wiring task (ts-ec0c9f).

### Replace prompt, don't just inject context
We set `output.prompt` to fully replace opencode's default compaction prompt. We also push task ID and role into `output.context` as structured data. Full control over the summary format.

### Agent writes to prog, not the hook
The hook fires BEFORE the summary is generated — it doesn't have the compacted text. So the compaction prompt includes an instruction: "After generating the summary, persist it with `prog log <task-id> 'Compaction: <summary>'`". The agent does the write as part of its post-compaction actions.

## Testing Strategy

Run the same agent test from ts-39f50e: spawn a worker on ts-b30b98 with the compaction hook installed. The previous session was 129+ events — long enough that compaction may trigger. Observe whether the compacted agent continues coherently and whether prog gets updated.

## Open Questions

- Does the hook have access to the full conversation history in `input`, or just metadata? Need to verify the `experimental.session.compacting` input shape.
- Should the compaction prompt include the full worker state machine, or just the handoff summary instructions? Leaning toward just handoff — the worker prompt is already in the compacted context.
- If compaction happens multiple times in one session, prog gets multiple log entries. Is that noise or useful history?
