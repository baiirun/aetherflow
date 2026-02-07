---
status: pending
priority: p2
issue_id: "005"
tags: [code-review, security, terminal]
dependencies: []
---

# Strip ANSI escape sequences from task titles and log messages before terminal output

## Problem Statement

Task titles (`TaskTitle`), log messages (`LastLog`), and queued task titles are printed directly to the terminal via `fmt.Printf` in `printStatus()`. These values originate from `prog show --json`, which contains user-authored task data. If a task title or log message contains ANSI escape sequences (e.g., `\x1b[2J` to clear screen, `\x1b]0;evil\x07` to change window title), they will be interpreted by the terminal emulator.

**Attack scenario:** A team member creates a task with a title containing ANSI escapes. When another developer runs `af status`, their terminal renders the escape sequences — potentially clearing the screen, changing colors, or displaying misleading content.

## Findings

- `printStatus` prints `a.LastLog`, `a.TaskTitle`, and `t.Title` without sanitization
- Only `truncate()` is applied, which doesn't strip escape sequences
- Values come from `prog show --json` → user-authored data
- Severity assessed as MEDIUM by security-sentinel (requires task data control, but realistic in team settings)

**Affected file:** `cmd/af/cmd/status.go:58-73, 84-87`

## Proposed Solutions

### Option 1: Strip ANSI escapes before display (Recommended)

**Approach:** Add a `stripANSI` function and apply it to all user-controlled strings before printing.

```go
import "regexp"

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\(B`)

func stripANSI(s string) string {
    return ansiEscape.ReplaceAllString(s, "")
}
```

Apply before `truncate()`:
```go
summary = stripANSI(summary)
summary = truncate(summary, 50)
```

**Pros:**
- Eliminates terminal injection risk
- Simple regex, well-understood pattern
- No user-visible behavior change for normal titles

**Cons:**
- Regex compiled at package level (negligible cost)

**Effort:** 20 minutes
**Risk:** Low

### Option 2: Use %q formatting

**Approach:** Use Go's `%q` verb which escapes non-printable characters.

**Pros:** Zero new code
**Cons:** Ugly output — wraps everything in Go-style quotes with `\x1b` visible

**Effort:** 5 minutes
**Risk:** Low

## Recommended Action

Option 1. Strip ANSI escapes in the display helpers. Apply to both agent summary and queue title. Add a test case.

## Acceptance Criteria

- [ ] ANSI escape sequences stripped from task titles and log messages
- [ ] Test case with embedded ANSI escapes
- [ ] Normal ASCII titles unaffected

## Work Log

### 2026-02-07 - Security Review Discovery

**By:** Claude Code

**Actions:**
- Identified terminal injection risk during security audit
- Assessed as MEDIUM severity (requires task data control, realistic in team settings)
