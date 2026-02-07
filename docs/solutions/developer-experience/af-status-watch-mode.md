---
module: daemon
date: 2026-02-07
problem_type: developer_experience
component: tooling
symptoms:
  - "af status only shows single snapshot, no continuous monitoring"
  - "operators must manually re-run command to monitor agent swarm"
  - "no built-in watch mode for pool state or queue changes"
root_cause: missing_tooling
resolution_type: tooling_addition
severity: medium
tags:
  - cli
  - watch-mode
  - signal-handling
  - ticker-panic
  - go
  - status-command
  - observability
---

# af status Lacks Continuous Monitoring Mode

## Problem

The `af status` command showed a point-in-time snapshot of the agent swarm, requiring operators to manually re-run the command to see changes. When monitoring long-running agent work or watching queue drain, operators needed a `watch(1)`-style continuous view that polls the daemon and redraws the terminal. The feature needed to work for both the swarm overview (`af status -w`) and single-agent detail with tool call timeline (`af status <agent> -w`).

## Environment

- Module: `cmd/af/cmd` (CLI), `internal/client` (RPC client)
- Go version: 1.25.5
- Affected components: `status.go` command definition, watch loop, rendering
- Date: 2026-02-07

## Symptoms

Not a bug fix -- this was a missing feature. The symptom was UX friction: operators had to repeatedly type `af status` to monitor swarm state during agent work sessions.

## What Didn't Work

**Direct solution:** The implementation was straightforward. A 4-agent code review (code-reviewer, simplicity-reviewer, grug-brain, security-sentinel) caught 6 issues before merge, all fixed in the same commit.

## Solution

### Flag registration

```go
statusCmd.Flags().BoolP("watch", "w", false, "Continuously refresh the display")
statusCmd.Flags().Duration("interval", 2*time.Second, "Refresh interval for watch mode")
```

### Mutual exclusion (simple if-statement, not cobra group)

```go
if asJSON {
    fmt.Fprintf(os.Stderr, "error: --watch and --json cannot be combined\n")
    os.Exit(1)
}
```

### Watch loop with interval validation and signal handling

```go
const minWatchInterval = 500 * time.Millisecond

func runStatusWatch(c *client.Client, args []string, interval time.Duration, cmd *cobra.Command) {
    if interval < minWatchInterval {
        fmt.Fprintf(os.Stderr, "error: --interval must be at least %s\n", minWatchInterval)
        os.Exit(1)
    }

    // Read flags once -- they don't change between ticks.
    limit, _ := cmd.Flags().GetInt("limit")

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    defer signal.Stop(sigCh)

    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        clearScreen()

        if len(args) == 1 {
            detail, err := c.StatusAgent(args[0], limit)
            if err != nil {
                fmt.Printf("error: %v\n", err)
            } else {
                printAgentDetail(detail)
            }
        } else {
            status, err := c.StatusFull()
            if err != nil {
                fmt.Printf("error: %v\n", err)
            } else {
                printStatus(status)
            }
        }

        fmt.Printf("\nRefreshing every %s. Press Ctrl+C to exit.", interval)

        select {
        case <-sigCh:
            fmt.Println()
            return
        case <-ticker.C:
        }
    }
}
```

### Review findings fixed before merge

| Issue | Fix | Why |
|---|---|---|
| `time.NewTicker` panics on `<=0` | 500ms minimum floor | Runtime panic prevention; also avoids daemon DoS |
| Only `os.Interrupt` handled | Added `syscall.SIGTERM` | Process managers send SIGTERM, not SIGINT |
| Errors to stderr cleared by `clearScreen` | Errors go to stdout | ANSI clear hits the same stream, so errors stay visible for full tick |
| `--limit` re-parsed every tick | Read once before loop | Flags are immutable; avoid per-tick overhead and leaking `*cobra.Command` |
| Render helpers added indirection | Inlined in loop body | Locality of behavior -- loop readable without jumping |
| No-op `TestClearScreen` | Removed | Tested nothing; ANSI output can't be meaningfully unit-tested |

## Why This Works

- **Immediate first render:** The `for` loop body runs before the first `select`, so users see output instantly.
- **Non-fatal errors in loop:** Transient daemon failures print an error and continue -- the next tick may succeed.
- **Clean shutdown:** Buffered signal channel (size 1) ensures signals aren't dropped between ticks. `signal.Stop` + `ticker.Stop` via defers prevent resource leaks.
- **ANSI clear vs shell-out:** `\x1b[H\x1b[2J` is portable and avoids forking a `clear` process every 2 seconds.
- **Existing render functions reused:** `printStatus` and `printAgentDetail` are called directly -- no separate "watch renderer."

## Prevention

- **Always guard `time.NewTicker`** with a minimum bound when interval comes from user input. The stdlib panics on `<= 0`.
- **Handle both SIGINT and SIGTERM** in any long-running loop. SIGINT covers Ctrl+C; SIGTERM covers process managers (systemd, Docker, k8s).
- **Use buffered signal channels** (size >= 1). Unbuffered channels can miss signals delivered between receives.
- **Read immutable inputs once before a loop**, not inside it. Flags, config, and environment don't change during execution.
- **Keep errors visible in watch mode** by printing to the same stream that gets cleared, so they're visible for the full tick duration.
- **Extract the one-shot path to its own function** when adding watch mode to an existing command.

## Related Issues

- See also: [post-review-hardening-findings-daemon-20260207.md](../best-practices/post-review-hardening-findings-daemon-20260207.md) -- companion doc from the same session covering the `af status <agent>` detail view review findings.
