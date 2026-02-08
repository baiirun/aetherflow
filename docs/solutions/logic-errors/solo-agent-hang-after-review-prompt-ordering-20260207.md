---
module: daemon
date: 2026-02-07
problem_type: logic_error
component: tooling
symptoms:
  - "Agent logs show implementation complete but no merge or prog done in solo mode"
  - "af status shows agent running at 0% CPU with no network connections or log output"
  - "Tasks remain in_progress indefinitely after agent completes review subagents"
  - "Pattern repeats across multiple agent spawns on the same task"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - agent-prompt
  - solo-mode
  - opencode
  - daemon
  - process-hang
  - subagent
  - prompt-ordering
  - land-sequence
  - go
---

# Solo Agent Hang After Review: Prompt Ordering Fix

## Problem

Autonomous worker agents in the aetherflow daemon completed their implementation work (tests passing, review subagents launched and returned) but never merged to main or called `prog done` in solo mode. Tasks stayed stuck as `in_progress` forever with the agent either dying during review or hanging at 0% CPU after review subagents returned.

## Environment

- Module: `internal/daemon` (prompts/worker.md, prompt.go)
- Go version: 1.25.5
- Affected components: Worker agent prompt (land section), opencode process lifecycle
- Date: 2026-02-07

## Symptoms

- Agent logs showed "Implementation complete. All tests passing." but no merge or done
- `af status` showed agent as running but 0% CPU, no network connections, no new log output for 8+ minutes
- Task remained `in_progress` in prog indefinitely
- The pattern repeated across multiple agent spawns on the same task

## Investigation

1. Parsed the agent JSONL log file (`.aetherflow/logs/ts-b69941.jsonl`) to trace the agent's execution
2. Found the first agent was killed externally while review subagents were running (daemon was stopped)
3. Found the second agent (`rapid_oracle`) completed all 5 review subagents but then froze — process alive at 0% CPU with no network connections
4. Checked token usage at the end (~110k tokens) — high but not at any known limit
5. Third agent (`frost_turret`) on a fresh daemon restart successfully completed the full cycle

## What Didn't Work

**First attempt:** Assumed context exhaustion was the cause since ~110k tokens is high. But the process was alive at 0% CPU — context exhaustion would show the agent attempting to continue and failing, not silently freezing.

**Second attempt:** Killing and respawning the agent on the same daemon instance reproduced the hang. Only a fresh daemon process (full restart) allowed the third agent to complete.

## Solution

Two changes addressed the compounding issues:

### 1. Reordered the land section in worker.md

The `land` section in `internal/daemon/prompts/worker.md` had `compound-auto` (documentation/learnings capture) as step 2, between final verification and the merge/done steps. This meant even if the agent survived review, it would enter compound (another multi-step documentation process with multiple tool calls) before reaching the critical merge + `prog done` steps.

```markdown
## Before (vulnerable ordering)

### Land
1. Final verification (tests, lint)
2. Compound (documentation, learnings capture)  <!-- multi-step, can exhaust context -->
3. Merge to main
4. Push
5. Cleanup branch
6. prog done

## After (ship-first ordering)

### Land
1. Final verification (tests, lint)
2. Merge to main                                <!-- critical path first -->
3. Push
4. Cleanup branch
5. prog done
6. Compound (documentation, learnings capture)  <!-- optional enrichment last -->
```

If the agent runs out of context or hangs during compound, no harm done — the task is already complete and marked done.

### 2. Updated step numbering in prompt.go

Both `landStepsNormal` and `landStepsSolo` constants in `internal/daemon/prompt.go` were renumbered to start at step 2 (was step 3) since compound moved from step 2 to the end of the sequence.

```go
// Before
const landStepsNormal = `3. Create PR...`
const landStepsSolo   = `3. Merge to main...`

// After
const landStepsNormal = `2. Create PR...`
const landStepsSolo   = `2. Merge to main...`
```

### 3. Fresh process restart

The opencode process hang was resolved by killing the daemon and starting a fresh one. The third agent on the fresh process completed the full cycle including review, fix, and land.

## Why This Works

**Prompt ordering:** The fundamental insight is that agent prompts define a pipeline, and agents can fail or hang at any step. Critical side effects (merge, done) must come before optional enrichment (documentation, learnings). This is the same principle as writing the WAL before applying the transaction — ensure the critical state transition happens before doing nice-to-have work.

**Process hang:** After processing the combined output from 5-6 parallel review subagents (Task tool calls), the opencode process got stuck in an internal state. This is likely an opencode bug when processing multiple large parallel subagent responses — the process was alive but at 0% CPU with no file descriptors open for network connections. A fresh process doesn't carry whatever corrupted internal state caused the hang.

## Prevention

- **Critical actions before optional enrichment:** In agent prompts, merge/done/deploy steps should always come immediately after verification. Documentation, learnings capture, and other enrichment steps go last — if the agent dies during enrichment, the task is already complete.
- **Monitor for idle agents:** When an agent has been idle (no log output) for more than 5 minutes after subagent completion, it's likely hung. Kill and respawn on a fresh process.
- **Fresh process as recovery path:** When an opencode process hangs after parallel subagent calls, killing and respawning on a fresh daemon process is the recovery path. Respawning on the same daemon instance may reproduce the hang.
- **Limit parallel subagent fan-out:** 5-6 parallel review subagents returning large outputs appears to trigger the opencode hang. Consider sequential review or batching if the problem recurs.

## Related Issues

- See also: [orphaned-in-progress-tasks-reclaim-on-startup](../runtime-errors/orphaned-in-progress-tasks-reclaim-on-startup-20260207.md) — daemon startup reclaim for tasks stuck in_progress after daemon crash
- See also: [reconciler-correctness-and-solo-mode-hardening](./reconciler-correctness-and-solo-mode-hardening-20260207.md) — solo mode merge instructions and reconciler fixes from the same session
