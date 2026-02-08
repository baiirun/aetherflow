---
module: daemon
date: 2026-02-07
problem_type: runtime_error
component: tooling
symptoms:
  - "af status panics with nil pointer dereference at status.go:259"
  - "fetchQueue called with nil Runner causes SIGSEGV"
  - "Config.Runner (yaml:\"-\" field) never set by CLI layer"
root_cause: config_error
resolution_type: code_fix
severity: high
tags:
  - nil-pointer
  - panic
  - config
  - runner
  - dependency-injection
  - daemon-startup
  - go
---

# Nil Pointer Panic in Status Handler When Runner Not Set

## Problem

Running `af status` against a running daemon causes a nil pointer panic. The `fetchQueue` function in `status.go` receives a nil `CommandRunner` because `Config.Runner` was never populated — the CLI layer doesn't know about it and YAML unmarshaling skips it (`yaml:"-"`).

## Environment

- Module: daemon (internal/daemon)
- Go Version: 1.25.5
- Affected Component: Status RPC handler, Config initialization, daemon.New()
- Date: 2026-02-07

## Symptoms

- `af status` on a running daemon produces:
  ```
  panic: runtime error: invalid memory address or nil pointer dereference
  [signal SIGSEGV: segmentation violation]
  goroutine X [running]:
  .../internal/daemon.fetchQueue(...)
      .../internal/daemon/status.go:259
  ```
- Only happens when `status.full` RPC is called — the pool itself works fine because `NewPool` had its own nil guard for Runner
- The daemon starts and runs agents normally, masking the bug until status is queried

## What Didn't Work

**Direct solution:** The problem was identified and fixed on the first attempt by tracing the nil pointer from the stack trace back to Config initialization.

## Solution

Set `Runner` and `Starter` defaults in `daemon.New()` before storing the config, so every code path that reads `d.config.Runner` gets a valid value.

**Code change in `internal/daemon/daemon.go`:**

```go
// Before: Runner and Starter not set — nil for any code path using d.config directly
func New(cfg Config, log *slog.Logger) *Daemon {
    // ... used cfg.Runner (nil) in status handlers
}

// After: defaults applied in New() before config is stored
func New(cfg Config, log *slog.Logger) *Daemon {
    if cfg.Runner == nil {
        cfg.Runner = ExecCommandRunner
    }
    if cfg.Starter == nil {
        cfg.Starter = ExecProcessStarter
    }
    // ... now d.config.Runner is always valid
}
```

## Why This Works

The `Config` struct has two fields tagged `yaml:"-"`:

```go
type Config struct {
    // ... yaml-serializable fields ...
    Runner  CommandRunner  `yaml:"-"`
    Starter ProcessStarter `yaml:"-"`
}
```

These are dependency-injection points for testing — they're never set by YAML unmarshaling or CLI flags. `NewPool` already had its own nil guard defaulting to `ExecCommandRunner`, so the pool worked fine. But `daemon.handleStatusFull` passed `d.config.Runner` directly to `BuildFullStatus` → `fetchQueue`, which called it without a nil check.

The fix applies defaults at the `Daemon` construction boundary (`New()`), so every downstream consumer — pool, status handler, future code — gets a valid Runner. This is better than sprinkling nil checks at every call site.

## Prevention

- **Set defaults at the construction boundary.** When a struct has optional/injectable fields, ensure they have sensible defaults in the constructor — don't rely on consumers to nil-check.
- **`yaml:"-"` fields are invisible to config loading.** Any `yaml:"-"` field must be explicitly initialized in code. Treat these as required constructor parameters that happen to have defaults.
- **Test the full RPC surface.** The status handler wasn't exercised in integration tests — adding a test that calls `status.full` on a running daemon would have caught this immediately.
- **Prefer dependency inversion via constructor, not struct field.** If Runner had been a required parameter to `New()`, the compiler would have caught the omission.

## Related Issues

- See also: [daemon-fails-outside-repo-root](./daemon-fails-outside-repo-root-embed-prompts-20260207.md) — another Config initialization issue where defaults weren't applied correctly
