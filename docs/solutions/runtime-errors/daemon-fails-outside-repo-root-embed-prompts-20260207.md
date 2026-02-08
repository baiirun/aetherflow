---
module: daemon
date: 2026-02-07
problem_type: runtime_error
component: tooling
symptoms:
  - "af daemon start fails with: prompt-dir must contain worker.md: stat prompts/worker.md: no such file or directory"
  - "daemon only works when started from the aetherflow repo root directory"
  - "detached daemon (-d) fails because cwd is inherited from caller"
root_cause: config_error
resolution_type: code_fix
severity: high
tags:
  - embed
  - go-embed
  - prompts
  - cwd-dependency
  - daemon-startup
  - config
  - go
---

# Daemon Fails Outside Repo Root: Embed Prompts into Binary

## Problem

Running `af daemon start --project X` from any directory other than the aetherflow repo root fails because prompt templates (`worker.md`, `planner.md`) are read from `./prompts/` relative to the daemon's working directory. This makes the binary non-portable — it can only run from where it was built.

## Environment

- Module: daemon (internal/daemon)
- Go Version: 1.25.5
- Affected Component: Config validation, RenderPrompt, pool spawn/respawn
- Date: 2026-02-07

## Symptoms

- `af daemon start --project eldspire-hexmap` from `/Users/.../eldspire-hexmap/` produces:
  ```
  error: prompt-dir "/Users/.../eldspire-hexmap/prompts" must contain worker.md:
  stat /Users/.../eldspire-hexmap/prompts/worker.md: no such file or directory
  ```
- Detached mode (`-d`) inherits the caller's cwd, making the problem intermittent
- Config validation resolves the relative `prompts` path to an absolute path using `filepath.Abs(cwd)`, so the error message points at the wrong directory

## What Didn't Work

**Direct solution:** The problem was identified and fixed on the first attempt. The root cause was clear from the error message — the prompts are aetherflow's own agent protocol (not user-customizable templates), so they should ship with the binary.

## Solution

Embed prompt templates into the binary using Go's `//go:embed` directive. Make the filesystem path an optional override for development.

**Key changes:**

1. Moved prompt files from `prompts/` to `internal/daemon/prompts/` so the embed directive can reach them from the same package.

2. Created `internal/daemon/prompts_embed.go`:
```go
//go:embed prompts/worker.md prompts/planner.md
var promptsFS embed.FS
```

3. Updated `RenderPrompt` to branch on `promptDir`:
```go
// Before: always read from filesystem
data, err = os.ReadFile(filepath.Join(promptDir, filename))

// After: embedded by default, filesystem override when configured
if promptDir == "" {
    data, err = fs.ReadFile(promptsFS, "prompts/"+filename)
} else {
    data, err = os.ReadFile(filepath.Join(promptDir, filename))
}
```

4. Updated `Config`:
   - Removed `DefaultPromptDir = "prompts"` constant
   - `PromptDir` empty (zero value) = use embedded prompts
   - `PromptDir` set = filesystem override with validation
   - Added startup log: "using embedded prompts" vs "using filesystem prompts"

5. Updated all tests to use empty `PromptDir` (embedded) by default.

## Why This Works

The prompts (`worker.md`, `planner.md`) are aetherflow's own agent protocol — they define how worker and planner agents behave. They're not user-customizable templates that vary per project. Embedding them means:

1. **No cwd dependency** — the binary works from any directory
2. **Atomic versioning** — prompts and daemon code ship together, so protocol changes don't require coordinating file deployments
3. **Zero-config default** — `af daemon start --project X` just works without needing a `prompts/` directory
4. **Escape hatch preserved** — setting `prompt_dir` in `.aetherflow.yaml` or via config lets developers iterate on prompts without rebuilding

The Go `embed.FS` is read-only and compiled into the binary at build time, adding negligible size (~10KB for both prompt files).

## Prevention

- **Prefer `//go:embed` for static assets** that ship with the binary (prompts, templates, default configs). Only use filesystem reads when the content genuinely varies per deployment.
- **Test from non-repo directories.** The original tests all ran from the repo root, so the relative path `"prompts"` always resolved correctly. Testing from another directory would have caught this immediately.
- **Use Go zero-value semantics** for optional config: `""` = use default (embedded), non-empty = override. This plays well with YAML unmarshaling and the config merge pattern.
- **Log which code path is active** at startup. The daemon now logs "using embedded prompts" or "using filesystem prompts" so operators can verify configuration without reading source code.

## Related Issues

- See also: [af-pool-flow-control](../developer-experience/af-pool-flow-control-drain-pause-resume-20260207.md) — uses the same Config/Validate pattern
- See also: [af-status-color-terminal-styling](../developer-experience/af-status-color-terminal-styling-20260207.md) — recent feature that uses embedded prompts via pool
- See also: [nil-pointer-status-handler](./nil-pointer-status-handler-runner-not-set-20260207.md) — another Config initialization issue from same session
- See also: [orphaned-in-progress-tasks-reclaim](./orphaned-in-progress-tasks-reclaim-on-startup-20260207.md) — daemon crash recovery from same session
- See also: [daemon-cross-project-shutdown-socket-isolation](../security-issues/daemon-cross-project-shutdown-socket-isolation-20260207.md) — cross-project socket isolation and path traversal hardening
