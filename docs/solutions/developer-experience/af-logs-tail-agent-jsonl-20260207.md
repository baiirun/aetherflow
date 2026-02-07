---
module: daemon
date: 2026-02-07
problem_type: developer_experience
component: tooling
symptoms:
  - "no way to see raw agent output in real time"
  - "operators must manually find and tail JSONL log files"
  - "af status shows parsed tool calls but not raw session output"
root_cause: missing_tooling
resolution_type: tooling_addition
severity: medium
tags:
  - cli
  - logs
  - tail
  - follow
  - jsonl
  - observability
  - rpc
  - go
---

# af logs: Tail Agent JSONL Logs

## Problem

The daemon captures agent stdout to JSONL log files (one per task), but operators had no CLI command to view these logs. The only way to see raw agent output was to manually find the log file path in `.aetherflow/logs/` and tail it. While `af status <agent>` shows parsed tool calls, it doesn't show the raw JSONL stream, which is essential for debugging agent behavior, prompt issues, and unexpected tool outputs.

## Environment

- Module: `cmd/af/cmd` (CLI), `internal/daemon` (RPC handler), `internal/client` (RPC client)
- Go version: 1.25.5
- Affected components: New `logs.go` command, new `logs.path` RPC method
- Date: 2026-02-07

## Symptoms

Not a bug fix -- this was a missing feature. The symptom was operational friction: operators debugging agent behavior had to know the log directory layout and manually construct `tail -f .aetherflow/logs/<taskID>.jsonl` commands.

## What Didn't Work

**Direct solution:** The design was straightforward: daemon returns the log file path via RPC, CLI tails it directly. A 4-agent code review (code-reviewer, simplicity-reviewer, grug-brain, security-sentinel) caught 6 issues before merge.

## Solution

### Architecture: daemon returns path, CLI tails directly

The key design decision was **not** streaming log content through the Unix socket. Instead, the daemon returns the file path and the CLI opens the file directly. This is simpler, avoids buffering/backpressure concerns, and lets the CLI use standard file I/O.

### RPC handler (`internal/daemon/logs.go`)

```go
func (d *Daemon) handleLogsPath(rawParams json.RawMessage) *Response {
    // Parse and validate params...
    
    // Find agent by name to get its task ID.
    agents := d.pool.Status()
    var taskID string
    for _, a := range agents {
        if string(a.ID) == params.AgentName {
            taskID = a.TaskID
            break
        }
    }
    if taskID == "" {
        d.log.Warn("logs.path: agent not found", "agent", params.AgentName)
        return &Response{Success: false, Error: fmt.Sprintf("agent %q not found in pool", params.AgentName)}
    }

    path := logFilePath(d.config.LogDir, taskID)
    // ... marshal and return
}
```

### CLI tail with follow mode (`cmd/af/cmd/logs.go`)

```go
// tailFile prints the last n lines, optionally following new output.
func tailFile(path string, n int, follow bool) error {
    f, err := os.Open(path)
    if err != nil {
        return fmt.Errorf("opening log file: %w", err)
    }
    defer f.Close()

    // Read all lines to get the tail. For agent logs this is fine --
    // they're bounded by session length and typically < 10k lines.
    lines, err := readAllLines(f)
    // ... print last n lines ...

    if !follow {
        return nil
    }
    return followFile(f)
}
```

### Follow mode with proper error handling

```go
func followFile(f *os.File) error {
    fmt.Fprintf(os.Stderr, "following %s (poll every %s, ctrl-c to stop)\n",
        f.Name(), followPollInterval)

    // ... signal handling, ticker setup ...

    for {
        for {
            line, err := reader.ReadString('\n')
            if len(line) > 0 {
                fmt.Print(line) // includes delimiter on complete lines
            }
            if err != nil {
                if err != io.EOF {
                    return fmt.Errorf("reading log file during follow: %w", err)
                }
                break // EOF -- poll again
            }
        }
        select {
        case <-sigCh:
            return nil
        case <-ticker.C:
        }
    }
}
```

### Review findings fixed before merge

| # | Finding | Fix | Source |
|---|---|---|---|
| 1 | `followFile` swallowed all errors as EOF | Distinguish `io.EOF` from real errors, return real errors | code-reviewer, grug |
| 2 | No indication follow mode is active | Added startup message to stderr | grug |
| 3 | Manual newline trim was 4 lines of complexity | Replaced with `fmt.Print(line)` -- ReadString includes delimiter | simplicity-reviewer |
| 4 | Test used `rune('0'+i)` for i>9 (produces non-digits) | Replaced with `fmt.Sprintf("line%d", i)` | code-reviewer |
| 5 | Test used fixed-size buffer read from pipe | Replaced with `io.ReadAll` | code-reviewer |
| 6 | No logging on agent-not-found error path | Added `d.log.Warn` for daemon observability | grug |

### Security analysis (no action required)

| Concern | Status | Why |
|---|---|---|
| Path traversal | Mitigated | `logFilePath` uses `filepath.Base(taskID)` to strip directory components |
| CLI trusts daemon path | Low risk | Path is constructed server-side from pool state, not from user input |
| Terminal injection from raw output | Acceptable | Consistent with `tail(1)` behavior; `af status` provides sanitized view |
| File handle leaks | Clean | `defer f.Close()`, `defer ticker.Stop()`, `defer signal.Stop(sigCh)` |
| Socket permissions | Pre-existing | `/tmp/aetherd.sock` permissions are a pre-existing concern, not introduced by this feature |

## Why This Works

- **Path-based, not stream-based:** The daemon returns a file path instead of streaming content through the socket. This avoids backpressure, buffering, and connection lifetime management. The CLI uses standard file I/O which is well-tested and understood.
- **`logFilePath` is the single source of truth:** Both the pool (when opening log files for writing) and the RPC handler (when returning the path) use the same `logFilePath(logDir, taskID)` function, so paths always match.
- **Follow mode is honest about what it's doing:** The startup message tells operators the file path, poll interval, and how to exit. Errors are surfaced immediately rather than silently retried.
- **Raw output is intentional:** The `logs` command is a raw viewer like `tail(1)`. The `status` command provides the sanitized, parsed view. Different tools for different purposes.

## Prevention

- **Distinguish `io.EOF` from real errors** in any polling/follow loop. A bare `if err != nil { break }` hides disk errors, permission changes, and file deletion behind silent infinite polling.
- **Add a startup message for any long-running CLI mode** (watch, follow, tail). Without it, operators can't tell if the tool is working or stuck.
- **Use `fmt.Print(line)` not `fmt.Println` with manual trim** when `ReadString` returns lines with delimiters. The delimiter is part of the return value -- stripping and re-adding it is unnecessary complexity.
- **Log error paths in daemon handlers**, not just success paths. "Agent not found" is the case operators will investigate; silence in daemon logs makes debugging harder.
- **Use `io.ReadAll` in tests**, not fixed-size buffer reads from pipes. A single `Read` call isn't guaranteed to return all data.

## Related Issues

- See also: [af-status-watch-mode.md](af-status-watch-mode.md) -- companion feature for continuous status monitoring, uses same signal handling and ticker patterns.
- See also: [post-review-hardening-findings-daemon-20260207.md](../best-practices/post-review-hardening-findings-daemon-20260207.md) -- documents the JSONL parser hardening that `af logs` builds on.
