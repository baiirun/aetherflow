---
module: daemon
date: 2026-02-07
problem_type: best_practice
component: tooling
symptoms:
  - "dead code and redundant fields remain after feature implementation"
  - "silent data loss when parsing malformed JSONL log lines"
  - "terminal injection possible via unstripped C0 control characters"
  - "ParseToolCalls blocks indefinitely on large log files with no cancellation"
  - "partial errors logged as count only, losing individual diagnostic messages"
root_cause: missing_validation
resolution_type: code_fix
severity: medium
tags:
  - code-review
  - dead-code
  - security-hardening
  - terminal-injection
  - context-cancellation
  - observability
  - jsonl-parsing
  - ansi-stripping
  - go
  - multi-agent-review
---

# Post-Implementation Review Findings: Dead Code, Missing Cancellation, Terminal Injection, and Silent Parse Failures

## Problem

After implementing `af status <agent-name>` with JSONL tool-call history parsing, a 7-agent code review surfaced 8 findings across dead code, security (terminal injection via incomplete ANSI stripping), observability gaps (silent data loss, missing error detail in logs), cancellation safety (unbounded parsing with no context), and unused data in both structs and display. All findings were in code that passed tests -- they represented correctness and hardening gaps invisible to happy-path testing.

## Environment

- Module: `internal/daemon` (JSONL parsing, agent detail builder, RPC handler), `internal/client`, `cmd/af/cmd`
- Go version: 1.25.5
- Affected components: `ParseToolCalls`, `BuildAgentDetail`, `handleStatusAgent`, `stripANSI`, `printAgentDetail`
- Date: 2026-02-07

## Symptoms

- `logFileForAgent()` function in `jsonl.go` had zero callers -- dead code adding maintenance surface
- `AgentDetail.ToolCount` always equaled `len(ToolCalls)`, duplicating information across daemon and client types
- `extractKeyInput` had 5 separate switch cases expressible as 2 -- unnecessary verbosity
- `ParseToolCalls` silently skipped malformed JSONL lines via bare `continue` with no signal when log data was lost
- `stripANSI` regex only handled CSI, OSC, and charset sequences -- CR, BS, DEL, DCS/PM/APC, NUL, and C0 controls passed through
- `ParseToolCalls` had no `context.Context` parameter -- large log files could block indefinitely
- `handleStatusAgent` logged error count but not individual messages
- `ToolCall.Title` was populated from JSONL but never rendered in CLI timeline

## What Didn't Work

**Direct solution:** All 8 findings were addressed in a single review-response pass. No iterative debugging required. The challenge was that standard development and testing didn't catch these issues -- they involve dead code analysis, security hardening beyond happy-path behavior, observability quality, and defensive programming patterns that only surface under adversarial or degraded conditions.

## Solution

### 1. Dead code removal (Finding 009)

Deleted the 12-line `logFileForAgent()` function from `jsonl.go`. `BuildAgentDetail` already performed its own agent lookup and path construction.

### 2. Redundant field removal (Finding 010)

```go
// Before:
type AgentDetail struct {
    AgentStatus
    ToolCalls []ToolCall `json:"tool_calls"`
    ToolCount int        `json:"tool_count"` // always len(ToolCalls)
    Errors    []string   `json:"errors,omitempty"`
}

// After:
type AgentDetail struct {
    AgentStatus
    ToolCalls []ToolCall `json:"tool_calls"`
    Errors    []string   `json:"errors,omitempty"`
}
```

Handler logging changed from `"tool_calls", detail.ToolCount` to `"tool_calls", len(detail.ToolCalls)`.

### 3. Switch case collapse (Finding 011)

```go
// Before:
case "read":
    return unquoteField(m, "filePath")
case "edit":
    return unquoteField(m, "filePath")
case "write":
    return unquoteField(m, "filePath")
case "glob":
    return unquoteField(m, "pattern")
case "grep":
    return unquoteField(m, "pattern")

// After:
case "read", "edit", "write":
    return unquoteField(m, "filePath")
case "glob", "grep":
    return unquoteField(m, "pattern")
```

### 4. Skipped line counting + context cancellation (Findings 012, 014)

```go
// Before:
func ParseToolCalls(path string, limit int) ([]ToolCall, error) {
    // ...
    for scanner.Scan() {
        if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
            continue // silent data loss
        }
    }
    return calls, nil
}

// After:
func ParseToolCalls(ctx context.Context, path string, limit int) ([]ToolCall, int, error) {
    var skipped int
    // ...
    for scanner.Scan() {
        if err := ctx.Err(); err != nil {
            return nil, skipped, err
        }
        if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
            skipped++
            continue
        }
    }
    return calls, skipped, nil
}
```

Caller wraps with timeout:

```go
parseCtx, parseCancel := context.WithTimeout(ctx, 5*time.Second)
defer parseCancel()

calls, skipped, err := ParseToolCalls(parseCtx, path, limit)
if skipped > 0 {
    errors = append(errors, fmt.Sprintf("skipped %d malformed lines in %s", skipped, path))
}
```

### 5. Comprehensive terminal injection stripping (Finding 013)

```go
// Before:
var ansiEscape = regexp.MustCompile(
    `\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\(B`)

// After:
var unsafeChars = regexp.MustCompile(
    `\x1b\[[0-9;]*[a-zA-Z]` +           // CSI sequences
    `|\x1b\][^\x07]*(?:\x07|\x1b\\)` +  // OSC (BEL or ST terminated)
    `|\x1b[P^_][^\x1b]*\x1b\\` +        // DCS/PM/APC sequences
    `|\x1b[^\[P^_\]]` +                 // Two-char escapes
    `|\r|\x08|\x7f` +                   // CR, BS, DEL
    `|[\x00-\x08\x0b\x0c\x0e-\x1a]`,   // C0 controls (preserve \t, \n)
)
```

Added 7 new test cases covering carriage return, backspace, DEL, DCS sequences, NUL bytes, and verifying tab/newline preservation.

### 6. Individual partial error logging (Finding 015)

```go
// After canonical log:
for _, e := range detail.Errors {
    d.log.Warn("status.agent.partial_error", "agent", params.AgentName, "error", e)
}
```

### 7. Title display in CLI (Finding 016)

```go
// Before:
fmt.Printf("  %6s  %-10s %s%s\n", relTime, tc.Tool, input, dur)

// After:
title := ""
if tc.Title != "" {
    title = " " + stripANSI(tc.Title)
}
fmt.Printf("  %6s  %-10s%s %s%s\n", relTime, tc.Tool, title, input, dur)
```

## Why This Works

**Dead code and redundancy (009, 010, 011):** Removing dead code and redundant fields reduces maintenance surface. `ToolCount` could drift from `len(ToolCalls)` if someone updated one but not the other. Collapsing identical switch cases makes grouping semantics explicit.

**Observable data loss (012):** Returning a `skipped` count converts an invisible failure mode into a measurable signal. The warning propagates to both operators (via logs) and users (via CLI output).

**Terminal injection defense (013):** Agent-controlled content flowing through JSONL into a terminal is a trust boundary crossing. The comprehensive regex strips all C0 controls except tab and newline, closing the injection surface while preserving readable output.

**Cancellation safety (014):** The per-iteration `ctx.Err()` check is O(1) (single atomic read), adding negligible overhead while guaranteeing the handler respects timeouts. The 5-second timeout is scoped narrowly to parsing, not the full request lifecycle.

**Operational observability (015):** Canonical log preserved for dashboarding; per-error warn lines supplement for debugging. Follows "wide events for querying, detailed events for investigation."

**Data completeness (016):** Title was already being parsed and serialized -- not displaying it meant CLI was strictly less informative than `--json | jq`.

## Prevention

- **Dead code:** Run `staticcheck` or `golangci-lint` with the `unused` linter in CI to catch unexported functions with zero callers
- **Silent data loss:** Adopt a convention: every `continue` in a parsing loop must either increment a counter or emit a log. Flag bare `continue` in `for scanner.Scan()` blocks during review
- **Terminal injection:** Any function sanitizing untrusted content for terminal display should have tests covering the full C0/C1 control character space, not just common ANSI sequences. Consider a dedicated library over custom regex
- **Context propagation:** Project rule: any function performing I/O or iterating over unbounded input must accept `context.Context` as first parameter. Enforceable via `contextcheck` linter
- **Observability:** When logging a count of items, ask: "Can an operator diagnose from this log alone?" If not, add individual-item logging
- **Unused data in display:** When adding a field to a struct, trace its path from source through all consumers. Partially-surfaced data creates confusion

## Related Issues

- See also: [daemon-cross-project-shutdown-socket-isolation](../security-issues/daemon-cross-project-shutdown-socket-isolation-20260207.md) — cross-project socket isolation and path traversal hardening (also used multi-agent review)
- See also: [reconciler-correctness-and-solo-mode-hardening](../logic-errors/reconciler-correctness-and-solo-mode-hardening-20260207.md) — second round of multi-agent review findings (stale refs, injection, error matching, solo mode)
