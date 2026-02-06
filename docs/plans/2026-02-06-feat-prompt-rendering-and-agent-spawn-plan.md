---
title: "feat: Prompt rendering and agent spawn via opencode run"
type: feat
date: 2026-02-06
---

# Prompt rendering and agent spawn via opencode run

## Overview

Wire prompt rendering into the daemon's spawn path so agents receive their role prompt as the initial message to `opencode run`. No plugin, no env vars in the prompt, no system.transform hook.

## Problem Statement

The daemon spawns agents but doesn't pass them a role prompt. The spawn command is `opencode run` with env vars, but the agent has no instructions about what it is or what to do. The prompt templates exist (`prompts/worker.md`, `prompts/planner.md`) and `RenderPrompt()` exists in `prompt.go`, but nothing connects them to the spawn path.

## Proposed Solution

The daemon renders the prompt template at spawn time and passes it as the message argument to `opencode run`:

```
opencode run "<rendered prompt with task_id baked in>"
```

The `ProcessStarter` interface changes to accept a prompt string alongside the spawn command. The prompt becomes the first positional argument to `opencode run`.

### What changes

1. **`ProcessStarter` signature** -- replace env with prompt:
   ```go
   type ProcessStarter func(ctx context.Context, spawnCmd string, prompt string) (Process, error)
   ```

2. **`ExecProcessStarter`** -- append the prompt as an argument, no custom env:
   ```go
   // spawnCmd = "opencode run"
   // parts becomes ["opencode", "run", "<rendered prompt>"]
   parts := strings.Fields(spawnCmd)
   parts = append(parts, prompt)
   cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
   // Inherits parent env only, no AETHERFLOW_* injection
   ```

3. **`spawn()`** -- call `RenderPrompt()` before spawning, pass result to starter

4. **`respawn()`** -- same: render prompt (role and task ID are known), pass to starter

5. **Config** -- add `PromptDir` field (path to prompts/ directory, defaults to `prompts/`)

6. **Env vars** -- remove all `AETHERFLOW_*` env vars from the spawn path. The agent doesn't use them (it gets task ID from the prompt, project from `prog show`). The daemon tracks agent-to-task mapping in its own `pool.agents` map, not via env vars. If a future plugin needs env vars, it can add them then. YAGNI.

### What stays the same

- `RenderPrompt()` in `prompt.go` -- already does the right thing (reads file, replaces `{{task_id}}`)
- Prompt templates -- `prompts/worker.md` and `prompts/planner.md` with `{{task_id}}` as the only template var
- The agent self-serves task context via `prog show <task_id>` during orient
- Pool lifecycle (reap, crash respawn, slot tracking)

### What doesn't change in the prompts

The prompts reference `<project>` in a few places (for `prog add` and `prog context` commands). The agent reads the project from `prog show` output during orient. No env var or template var needed.

## Acceptance Criteria

- [ ] `ProcessStarter` takes `(ctx, spawnCmd, prompt)` -- no env parameter
- [ ] `ExecProcessStarter` passes the rendered prompt as an argument to the spawn command
- [ ] `spawn()` calls `RenderPrompt()` and passes the result to the starter
- [ ] `respawn()` does the same
- [ ] No `AETHERFLOW_*` env vars are set on spawned processes
- [ ] Config has a `PromptDir` field with a sensible default
- [ ] Existing pool tests updated for new `ProcessStarter` signature
- [ ] New test: `TestRenderPrompt` verifies template replacement
- [ ] New test: `TestRenderPromptMissingFile` verifies error on missing prompt file
- [ ] `go test ./internal/daemon/... -race -count=1` passes

## Dependencies & Risks

- `RenderPrompt()` already exists and has no tests -- adding tests here
- Changing `ProcessStarter` signature breaks all existing test fakes -- straightforward mechanical update (remove env param, add prompt param)
- Removing env vars: existing pool tests assert on env vars passed to the starter. Those assertions get deleted.
- The rendered prompt may be long (worker.md is ~126 lines). `opencode run` should handle long message arguments -- Go's `exec.Command` passes args directly (no shell), so no length limit beyond OS arg max (~256KB on most systems, prompt is ~4KB)
- Shell escaping: not a concern since `exec.Command` doesn't go through a shell. The prompt is a single argv element.

## References

- `internal/daemon/prompt.go` -- RenderPrompt function
- `internal/daemon/pool.go:159-224` -- spawn method
- `internal/daemon/pool.go:298-343` -- respawn method
- `internal/daemon/config.go` -- Config struct
- `prompts/worker.md` -- worker prompt template
- `prompts/planner.md` -- planner prompt template
