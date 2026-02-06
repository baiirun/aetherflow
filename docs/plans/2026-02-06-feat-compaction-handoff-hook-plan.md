---
title: "feat: Compaction handoff hook"
type: feat
date: 2026-02-06
task: ts-b54507
brainstorm: docs/brainstorms/2026-02-06-compaction-handoff-hook-brainstorm.md
---

# Compaction Handoff Hook

When an opencode session compacts, replace the default compaction prompt with aetherflow's handoff format so the agent's continuation knows: what task it's working on, what state it was in, what's been done, and what didn't work. Also instruct the agent to write the summary to prog so external observers have visibility.

## Acceptance Criteria

- [x] Plugin at `.opencode/plugins/compaction-handoff.ts` uses `experimental.session.compacting`
- [x] Sets `output.prompt` to the handoff summary instructions (from `prompts/handoff.md`)
- [x] Pushes task ID, role, commands, and compressed protocol into `output.context` so they survive compaction
- [x] Fetches messages via SDK `client.session.messages()` and parses task ID via regex
- [x] Regex matches all ID formats: `ts-`, `ep-`, etc. (2+ letter prefix + 4-12 hex chars, case-insensitive)
- [x] Role detected dynamically from prompt heading (`# Worker` / `# Planner`)
- [x] Compaction prompt instructs agent to persist via `prog desc` (current truth) + `prog log` (audit trail)
- [x] If no task ID is found (running outside aetherflow), the hook does nothing with debug log
- [x] Local plugin auto-discovered from `.opencode/plugins/` — no `opencode.json` registration needed
- [x] Logging at all decision branches (warn, debug, info)
- [x] `handoff.md` read per compaction (hot-reload), not cached at load time
- [ ] Verify: run an agent session on a real task, observe compaction behavior follows handoff format

## Context

- **Design doc:** `docs/swarm-feedback-loops.md` lines 181-215 — "Compaction and handoff are the same thing"
- **Brainstorm:** `docs/brainstorms/2026-02-06-compaction-handoff-hook-brainstorm.md`
- **Learning lrn-aa8951:** opencode auto-compacts in run mode, agents survive context limits
- **Learning lrn-b636f7:** Agent sandbox blocks external file access — all context must be in-project or in task description

## Key Design Decisions (Post-Review)

### SDK client for message access
The `experimental.session.compacting` hook provides only `{ sessionID }` in `input`, not messages. We use the SDK `client` (from plugin factory context) to fetch session messages via `client.session.messages()`. This was a P1 finding — the original implementation accessed `input.messages` which doesn't exist.

### No opencode.json needed
Local plugins in `.opencode/plugins/` are auto-discovered by opencode. The `"plugin"` config key is only for npm packages. Verified via smoke test.

### Cross-boundary contract
The `Task: {{task_id}}` format in `prompts/worker.md` line 8 and `prompts/planner.md` line 8 is machine-parsed by the plugin regex. Both prompt files now have a comment marking this as a contract: `<!-- Machine-parsed by .opencode/plugins/compaction-handoff.ts — do not change this format -->`.

### prog desc + prog log
The handoff prompt and compaction instructions use `prog desc` for current truth (what the next agent reads first) and `prog log` for the audit trail. This aligns with `swarm-feedback-loops.md:219-225`.

### Compressed protocol in context
Post-compaction agents need to know HOW to work, not just WHAT they're working on. The context block includes a compressed version of the worker protocol (state machine, stuck detection, checkpoint discipline).

## References

- OpenCode plugin docs: `experimental.session.compacting` hook — Context7 `/anomalyco/opencode`
- Plugin factory context: `{ project, client, $, directory, worktree }` — Context7 `/anomalyco/opencode`
- `prompts/handoff.md` — the handoff summary format
- `docs/swarm-feedback-loops.md:181` — "Compaction and handoff are the same thing"
- `docs/brainstorms/2026-02-06-compaction-handoff-hook-brainstorm.md` — design decisions
