---
status: complete
priority: p3
issue_id: "050"
tags: [code-review, observability, reliability]
dependencies: []
---

# Add logging for FormatEvent parse failures

## Problem Statement

`FormatEvent` returns empty string for 5 different failure modes, 3 of which indicate data corruption or malformed events from the plugin. These are silently swallowed with no logging, making it impossible to diagnose event pipeline issues.

## Findings

Silent failure modes:
1. Non-`message.part.updated` events → legitimate filter, OK
2. Empty `ev.Data` → legitimate, OK
3. Failed JSON unmarshal of envelope → **data corruption signal, needs logging**
4. Failed JSON unmarshal of part → **data corruption signal, needs logging**
5. Unknown `part.Type` in default switch → **feature gap signal, needs logging**

The caller in `handleEventsList` also silently drops empty strings (line 123-126).

## Proposed Solutions

### Option 1: Accept a logger parameter

**Approach:** Change `FormatEvent(ev SessionEvent)` to `FormatEvent(ev SessionEvent, log *slog.Logger)` and log warnings for cases 3-5.

**Pros:** Observable failures, easy to diagnose pipeline issues
**Cons:** Signature change, need to pass logger from all callers
**Effort:** 15 minutes
**Risk:** Low

### Option 2: Return (string, error)

**Approach:** Return errors for parse failures, let caller decide.

**Pros:** Most flexible, caller can log or handle
**Cons:** More invasive change, callers need to handle errors
**Effort:** 20 minutes
**Risk:** Low

## Recommended Action



## Acceptance Criteria

- [ ] Parse failures (cases 3-5) produce a log message
- [ ] Log includes event_type, session_id, and error
- [ ] Normal events are not affected
- [ ] Tests verify logging behavior

## Work Log

### 2026-02-19 - Code Review Discovery

**By:** Claude Code (tigerstyle-reviewer)
