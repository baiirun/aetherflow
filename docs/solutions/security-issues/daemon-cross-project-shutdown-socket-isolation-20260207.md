---
module: daemon
date: 2026-02-07
problem_type: security_issue
component: tooling
symptoms:
  - "daemon shuts down unexpectedly 60 seconds after spawning a new agent"
  - "shutting down log message with no corresponding signal or operator action"
  - "agent working on aetherflow codebase kills production daemon via shared socket"
root_cause: config_error
resolution_type: code_fix
severity: critical
tags:
  - daemon-isolation
  - unix-socket
  - process-group
  - setsid
  - path-traversal
  - socket-permissions
  - go
  - multi-agent-review
---

# Daemon Cross-Project Shutdown via Shared Socket and Signal Propagation

## Problem

The aetherflow daemon shut down unexpectedly after spawning a new agent. An agent working on the aetherflow codebase itself sent a shutdown RPC to the same global socket (`/tmp/aetherd.sock`) that the production daemon was listening on. Additionally, spawned agent processes shared the daemon's process group, allowing terminal signals to propagate between them.

## Environment
- Module: daemon
- Affected Component: daemon socket, process spawning (`pool.go`), all CLI commands
- Date: 2026-02-07

## Symptoms
- Daemon logs showed `"shutting down"` exactly 60 seconds after spawning `bolt_chunk` agent for task `ts-ea2b26`
- Two agents were running normally before the shutdown
- No operator action (Ctrl+C, `af daemon stop`) was performed
- Daemon was running in foreground terminal mode

## What Didn't Work

**Direct solution:** Root cause was identified through code analysis on the first attempt. The 60-second gap between spawn and shutdown was initially suspicious as a timeout, but investigation confirmed it was simply the time it took the other agent to reach a command that hit the shared socket.

## Solution

Three categories of fixes applied, informed by a 7-agent parallel code review:

### Fix 1: Process Group Isolation (Setsid)

Added `Setsid: true` to spawned agent processes so they get their own session, preventing terminal signal propagation between daemon and agents.

```go
// internal/daemon/pool.go — ExecProcessStarter
// Before (broken):
cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
cmd.Stdout = stdout
cmd.Stderr = os.Stderr

// After (fixed):
cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
cmd.SysProcAttr = &syscall.SysProcAttr{
    Setsid: true, // Own process group so terminal signals don't propagate to daemon
}
cmd.Stdout = stdout
cmd.Stderr = os.Stderr
```

### Fix 2: Project-Scoped Socket Paths

Changed from a global singleton socket to project-scoped sockets. Moved `SocketPathFor` to `internal/protocol` as the single source of truth, with `filepath.Base` sanitization to prevent path traversal.

```go
// internal/protocol/socket.go — single source of truth
func SocketPathFor(project string) string {
    if project == "" {
        return DefaultSocketPath
    }
    safe := filepath.Base(project) // prevents ../../etc/evil traversal
    if safe == "." || safe == "/" {
        return DefaultSocketPath
    }
    return fmt.Sprintf("/tmp/aetherd-%s.sock", safe)
}
```

Project name validation in `Config.Validate()`:

```go
// internal/daemon/config.go
var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// In Validate():
if !validProjectName.MatchString(c.Project) {
    return fmt.Errorf("project name %q contains invalid characters", c.Project)
}
```

### Fix 3: Defense in Depth (from code review)

- **Socket permissions:** `os.Chmod(path, 0700)` after socket creation — owner-only access
- **RPC logging:** `d.log.Debug("rpc request", "method", req.Method)` on every request, plus `d.log.Info("shutdown requested via RPC")` to distinguish signal vs RPC shutdowns
- **Config error handling:** `resolveSocketPath` warns on malformed YAML instead of silently falling back to the wrong socket
- **Persistent `--socket` flag:** Single registration on `rootCmd` instead of 8 per-command registrations

### CLI Socket Discovery

All CLI commands auto-discover the socket by reading the project name from `.aetherflow.yaml`:

```go
// cmd/af/cmd/root.go — resolveSocketPath
// Priority: explicit --socket flag → project from config → default
func resolveSocketPath(cmd *cobra.Command) string {
    if cmd.Flags().Changed("socket") {
        s, _ := cmd.Flags().GetString("socket")
        return s
    }
    // Minimal YAML parse — only needs project field, avoids importing daemon package
    data, err := os.ReadFile(configPath)
    if err == nil {
        var partial struct { Project string `yaml:"project"` }
        if err := yaml.Unmarshal(data, &partial); err != nil {
            fmt.Fprintf(os.Stderr, "warning: failed to parse %s: %v\n", configPath, err)
        } else if partial.Project != "" {
            return protocol.SocketPathFor(partial.Project)
        }
    }
    return protocol.DefaultSocketPath
}
```

## Why This Works

**Root cause 1 — Shared process group:** When the daemon runs in foreground, it and all child processes share the same terminal session/process group. Any signal sent to the group (Ctrl+C, SIGHUP) hits all processes. `Setsid: true` creates a new session for each child, breaking this propagation.

**Root cause 2 — Global socket singleton:** `/tmp/aetherd.sock` was a hardcoded global path. Any `af` command on the machine — including an agent running tests or invoking `af daemon stop` against the aetherflow codebase — would connect to the production daemon's socket and could shut it down. Project-scoped paths (`/tmp/aetherd-{project}.sock`) ensure each daemon instance has its own socket.

**Root cause 3 — Path traversal:** The project name flowed unsanitized into the socket path. A malicious `.aetherflow.yaml` with `project: "../../run/systemd"` could create a socket at an arbitrary filesystem path. `filepath.Base` strips directory components, and the regex validation in `Config.Validate()` rejects any project name with slashes, spaces, or other dangerous characters.

**Root cause 4 — Silent errors:** `resolveSocketPath` previously swallowed config parse errors with `_ = LoadConfigFile(...)`. A malformed YAML file would silently fall back to the default socket, connecting the CLI to the wrong daemon — recreating the original bug in a different form.

## Prevention

- **Single source of truth for protocol conventions:** `SocketPathFor` lives in `internal/protocol`, imported by both daemon and client. No duplication to drift.
- **Validate at the boundary:** Project names are validated by regex in `Config.Validate()` before they reach `SocketPathFor`. Socket paths are sanitized with `filepath.Base` as defense in depth.
- **Log RPC requests:** Every request to the daemon is logged with its method name. Shutdown specifically logs `"shutdown requested via RPC"` so the cause is immediately visible.
- **Don't swallow errors:** Config parse failures are surfaced as warnings. Missing files are silently acceptable; broken files are not.
- **Process isolation by default:** All spawned processes get `Setsid: true`. The daemon itself uses `Setsid: true` when started with `-d` (detached mode).
- **Restrict socket permissions:** `os.Chmod(0700)` prevents other local users from issuing commands to the daemon.

## Code Review Process

This fix was developed iteratively:

1. **Initial fix** — `Setsid: true` + project-scoped sockets (basic implementation)
2. **7-agent parallel review** — code-reviewer, security-sentinel, architecture-strategist, code-simplicity-reviewer, grug-brain-reviewer, pattern-recognition-specialist, tigerstyle-reviewer
3. **Review findings** — 1 P1 (path traversal), 4 P2s (duplication, silent errors, missing logging, wrong dependency direction), 3 P3s (flag consolidation, socket permissions, error UX)
4. **All 8 findings fixed** — single pass, all tests passing

Key review insights:
- Security-sentinel and tigerstyle-reviewer both caught the path traversal in `SocketPathFor` that the initial implementation missed
- All 7 agents flagged the `SocketPathFor` duplication across packages — unanimous consensus
- Grug-brain-reviewer identified the "malformed YAML → wrong socket → 'not running' lie" scenario
- Architecture-strategist identified the wrong dependency direction (CLI importing daemon package)

## Related Issues

- See also: [daemon-fails-outside-repo-root-embed-prompts](../runtime-errors/daemon-fails-outside-repo-root-embed-prompts-20260207.md) — another Config initialization issue from same session
- See also: [post-review-hardening-findings](../best-practices/post-review-hardening-findings-daemon-20260207.md) — prior code review findings on the same daemon codebase
- See also: [orphaned-in-progress-tasks-reclaim](../runtime-errors/orphaned-in-progress-tasks-reclaim-on-startup-20260207.md) — daemon crash recovery from same session
