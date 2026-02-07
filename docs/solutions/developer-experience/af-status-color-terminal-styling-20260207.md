---
module: daemon
date: 2026-02-07
problem_type: developer_experience
component: tooling
symptoms:
  - "af status output is plain monochrome text, hard to scan visually"
  - "no way to disable colors for piped output or accessibility"
  - "column alignment breaks when ANSI color codes wrap format strings"
  - "term_unix.go uses macOS-only TIOCGETA ioctl, fails on Linux"
root_cause: missing_tooling
resolution_type: code_fix
severity: medium
tags:
  - terminal-colors
  - ansi
  - no-color
  - column-alignment
  - cross-platform
  - multi-agent-review
  - go
---

# af status: Color and Terminal Styling

## Problem

The `af status` output was plain monochrome text. With multiple agents, queue items, and warnings on screen, it was hard to visually scan and distinguish running agents from idle slots, queue items from errors, and agent IDs from task IDs. The DoD called for green (running), red (crashed/errors), dim (idle), and yellow (queue) with `NO_COLOR` and `--no-color` support.

## Environment

- Module: daemon CLI (`cmd/af/cmd/status.go`)
- Go Version: 1.25.5
- Affected Component: `af status`, `af drain/pause/resume`
- Date: 2026-02-07

## Symptoms

- Status output was a wall of monochrome text
- No color coding to distinguish agent states at a glance
- No `NO_COLOR` env var or `--no-color` flag support
- No terminal width detection for adaptive truncation

## What Didn't Work

**Direct solution with review fixes:** The initial implementation worked but a 5-agent review (code-reviewer, simplicity-reviewer, grug-brain, security-sentinel, performance-oracle) found 12 issues, 2 of which were P1 bugs.

## Solution

Created `internal/term` package with zero external dependencies and applied colors throughout the CLI.

### Color scheme

| Element | Color | Semantic |
|---------|-------|----------|
| Running agents, uptime, success | Green | Active/healthy |
| Crashed agents, errors, warnings | Red | Attention needed |
| Queue items, draining mode | Yellow | Pending/transitional |
| Idle slots, secondary info, timestamps | Dim | Background/inactive |
| Agent IDs, tool names | Cyan | Identifiers |
| Task IDs | Blue | References |
| Roles | Magenta | Categories |
| Labels ("Pool:", "Queue:") | Bold | Structure |

### internal/term package

```go
// Zero-dependency color package. Colors auto-disabled when:
// - NO_COLOR env var is set (any value, per https://no-color.org/)
// - stdout is not a terminal (piped/redirected)
// - Disable(true) called (from --no-color flag)

// sync.Once for one-time env detection, simple Mutex for Disable flag.
var (
    mu       sync.Mutex
    disabled bool
    initOnce sync.Once
    noColor  bool
)

func enabled() bool {
    initOnce.Do(func() {
        if _, ok := os.LookupEnv("NO_COLOR"); ok { noColor = true; return }
        if !isTerminal(os.Stdout) { noColor = true }
    })
    mu.Lock()
    defer mu.Unlock()
    return !disabled && !noColor
}
```

### Column alignment with PadRight/PadLeft

The critical insight: `fmt.Printf("%-14s", term.Cyan(s))` pads by **byte length**, but ANSI codes add 9 invisible bytes. Columns become jagged.

```go
// WRONG: %-14s pads by bytes, ANSI codes are invisible but counted
fmt.Printf("%-14s", term.Cyan("agent-1"))  // 16 bytes, no padding added

// RIGHT: pad visible content first, then wrap in color
term.PadRight("agent-1", 14, term.Cyan)    // pads to 14 visible chars, then colors
```

The `PadRight` helper:

```go
func PadRight(s string, width int, color func(string) string) string {
    runes := []rune(s)
    if len(runes) >= width {
        return color(s)
    }
    padded := s + spaces(width-len(runes))
    return color(padded)
}
```

### Platform-specific terminal detection

Split into `term_darwin.go` and `term_linux.go` with correct ioctl constants:

```go
// term_darwin.go — uses syscall.TIOCGETA (macOS/BSD)
// term_linux.go — uses TCGETS = 0x5401 (Linux)
// term_windows.go — stub returning false/fallback
```

### --no-color flag via cobra.OnInitialize

```go
// OnInitialize runs before any PreRun hooks and doesn't participate
// in Cobra's override chain, so subcommands can freely set their
// own PersistentPreRun without breaking this.
cobra.OnInitialize(func() {
    if noColor, _ := rootCmd.Flags().GetBool("no-color"); noColor {
        term.Disable(true)
    }
})
```

## Why This Works

1. **PadRight/PadLeft** — Pads the visible string to the desired column width, then wraps the padded string in color. The terminal sees correct column widths because padding is in the visible content, not fighting ANSI codes.

2. **sync.Once** — Environment detection runs once (atomic fast path after first call), `disabled` flag checked under a simple Mutex. No RWMutex double-checked locking complexity.

3. **cobra.OnInitialize** — Runs before any command's `PreRun` hooks and doesn't participate in Cobra's `PersistentPreRun` override chain. Safe for subcommands to add their own `PersistentPreRun` later.

4. **Platform split** — `TIOCGETA` is macOS/BSD, `TCGETS` (0x5401) is Linux. The `!windows` build tag was insufficient. Separate files with `darwin`/`linux`/`windows` tags ensure correct ioctl constants per platform.

5. **stripANSI before truncate** — Always sanitize untrusted strings before truncating. If you truncate first, you might cut a string mid-ANSI-escape-sequence, creating an unterminated escape that leaks to the terminal.

## Prevention

- **Never use `%-Ns` with ANSI-wrapped values** — Always pad the plain string first, then colorize. Use `term.PadRight(s, width, term.Color)`.
- **Always `stripANSI` before `truncate`** — Stripping after truncation can create unterminated escape sequences.
- **Use `cobra.OnInitialize` for global flag wiring** — Not `PersistentPreRun`, which gets silently overridden by subcommands.
- **Use `sync.Once` for one-time detection** — Don't hand-roll double-checked locking with RWMutex when `sync.Once` exists.
- **Split platform files by OS** — `!windows` is not enough when macOS and Linux have different ioctl constants. Use explicit `darwin`/`linux`/`windows` build tags.
- **Always sanitize error strings** — Even if they come from internal sources, apply `stripANSI()` to all untrusted output paths to prevent terminal injection.
- **Add Windows stubs** — Even if not targeting Windows, a stub file prevents broken builds and provides graceful degradation.

## Review Findings Applied

A 5-agent parallel review produced 12 findings:

**P1 Critical (2):**
1. Column alignment broken — `%-14s` pads by bytes, not visible width. Fixed with `PadRight`/`PadLeft`.
2. `TIOCGETA` macOS-only — Linux build would fail at runtime. Split into platform-specific files.

**P2 Important (3):**
3. `PersistentPreRun` overrides subcommand hooks — replaced with `cobra.OnInitialize`.
4. `RWMutex` double-checked locking has TOCTOU race — replaced with `sync.Once`.
5. Error strings not sanitized — added `stripANSI()` to both error display loops.

**P3 Nice-to-have (7):**
6. Dead code: `white` constant, `Boldf` — removed.
7. `clearScreen()` bypasses term package — added explanatory comment.
8. `Disable()` doc misleading — clarified it doesn't override NO_COLOR.
9. Magic number `prefixCols = 47` — derived from named constants.
10. No positive color test — added `TestColorOutputWhenEnabled`.
11. No Windows stub — added `term_windows.go`.
12. Pool mode default-then-overwrite — made switch explicit.

## Related Issues

- See also: [af-status-watch-mode.md](./af-status-watch-mode.md) — watch mode uses clearScreen which intentionally bypasses term package
- See also: [af-pool-flow-control-drain-pause-resume-20260207.md](./af-pool-flow-control-drain-pause-resume-20260207.md) — pool mode colors in `printPoolModeResult`
- See also: [post-review-hardening-findings-daemon-20260207.md](../best-practices/post-review-hardening-findings-daemon-20260207.md) — multi-agent review pattern
