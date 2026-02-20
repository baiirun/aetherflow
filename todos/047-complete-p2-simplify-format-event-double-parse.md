---
status: complete
priority: p2
issue_id: "047"
tags: [code-review, simplification, performance]
dependencies: []
---

# Simplify FormatEvent — eliminate double-parse and LogEvent intermediary

## Problem Statement

`FormatEvent` in logfmt.go does three JSON unmarshals per event: envelope → anonymous part struct → copies into LogEvent → formatting helpers. The anonymous `part` struct and `LogEvent.Part` are structurally identical, creating a maintenance hazard (if one is updated, the other silently diverges). `LogEvent` was the deserialization target for the now-deleted `FormatLogLine()` — it should not have survived.

## Findings

- FormatEvent parses `ev.Data` → `envelope.Part` (RawMessage) → anonymous `part` struct → copies to `LogEvent`
- `LogEvent` is only used inside `FormatEvent` and by the formatting helpers (`formatText`, `formatToolUse`, `formatStepFinish`)
- The anonymous `part` struct (lines 71-96) mirrors `LogEvent.Part` field-for-field
- Can parse directly into `LogEvent.Part`, eliminating one unmarshal and the 9-line field copy block
- Alternatively, replace `LogEvent` with a simpler `eventPart` type and have formatters accept that directly
- Flagged by simplicity-reviewer, tigerstyle-reviewer, and pattern-recognition agents

## Proposed Solutions

### Option 1: Parse directly into LogEvent.Part (Recommended)

**Approach:** Remove the anonymous struct, unmarshal `envelope.Part` directly into `logEv.Part`.

**Pros:** Eliminates duplicate struct, removes 9-line copy, one fewer unmarshal
**Cons:** Minor
**Effort:** 20 minutes
**Risk:** Low

### Option 2: Replace LogEvent with eventPart, change formatter signatures

**Approach:** Define `eventPart` type, have formatters accept `(ts string, part eventPart)` instead of `(ts string, ev LogEvent)`.

**Pros:** Cleaner architecture, removes exported zombie type
**Cons:** Touches more functions (formatText, formatToolUse, formatStepFinish, formatToolOutput)
**Effort:** 30 minutes
**Risk:** Low

## Recommended Action



## Acceptance Criteria

- [ ] FormatEvent does at most 2 JSON unmarshals (envelope + part)
- [ ] No duplicate struct definitions
- [ ] All FormatEvent tests still pass
- [ ] `go build ./...` and `go test ./...` pass

## Work Log

### 2026-02-19 - Code Review Discovery

**By:** Claude Code (simplicity-reviewer + tigerstyle-reviewer)

**Actions:**
- Identified double-parse pattern in FormatEvent
- Confirmed LogEvent is only used internally
- Estimated ~50 LOC reduction from simplification
