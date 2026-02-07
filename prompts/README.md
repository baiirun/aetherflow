# Prompt Assembly

How the daemon turns prompt files into an agent's first message.

## Where prompts live

Prompt templates are embedded into the `af` binary at compile time from
`internal/daemon/prompts/*.md`. The daemon reads them from the embedded
filesystem by default — no files need to exist on disk at runtime.

To override prompts for development, set `prompt_dir` in `.aetherflow.yaml`
or pass `--prompt-dir` to `af daemon start`. The daemon will read from the
filesystem path instead of the embedded copies.

## Template variables

The only template variable is `{{task_id}}`. The daemon replaces it with the
actual task ID at spawn time before passing the rendered prompt to `opencode run`.

The agent reads everything else (project, description, DoD, learnings) from
`prog show <task_id>` during its orient step.

## Assembly flow

```
┌──────────┐                              ┌──────────────┐
│  Daemon   │                              │    Agent      │
└─────┬─────┘                              └───────┬───────┘
      │                                            │
      │  1. Read role prompt from embedded FS      │
      │     (or filesystem override if configured) │
      │                                            │
      │  2. Replace {{task_id}} with actual ID     │
      │                                            │
      │  3. Spawn: opencode run "<rendered prompt>" │
      │  ─────────────────────────────────────────> │
      │                                            │
      │                          4. Agent sees     │
      │                             prompt as its  │
      │                             first message  │
      │                                            │
      │                          5. Agent runs     │
      │                             prog show <id> │
      │                             to get details │
      └────────────────────────────────────────────┘
```

No plugin, no env vars, no system.transform hook. The rendered prompt is passed
directly as the message argument to `opencode run`.

## Prompt layers

The agent's context has three layers:

1. **opencode's default system prompt** — already present, not ours to modify
2. **role prompt** — from `opencode run "<rendered prompt>"` (worker.md or planner.md with `{{task_id}}` filled in)
3. **agent-fetched context** — the agent calls `prog show`, `prog context`, etc. during orient

Layer 1 is set by opencode. Layer 2 is the rendered template passed as the first
message. Layer 3 is fetched by the agent as its first action.

## Adding a new role

1. Create `internal/daemon/prompts/<role>.md` with the role prompt
2. Use `{{task_id}}` wherever the task ID should appear
3. Add the role constant to `role.go` and add it to the allowlist in `RenderPrompt` (`prompt.go`)
4. Add the file to the `//go:embed` directive in `prompts_embed.go`
5. Update the daemon's role inference to recognize the new role
