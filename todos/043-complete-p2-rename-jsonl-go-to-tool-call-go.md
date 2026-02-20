---
status: complete
priority: p2
issue_id: "043"
tags: [code-review, naming, cleanup]
dependencies: []
---

# Rename jsonl.go to tool_call.go

## Problem Statement

After commit f644f5f removed all JSONL parsing code, `internal/daemon/jsonl.go` contains only `ToolCall` type, `extractKeyInput()`, and `unquoteField()` — none of which relate to JSONL. The file name is actively misleading and violates locality of behavior: developers looking for tool call types won't look in `jsonl.go`.

## Findings

- `jsonl.go` is 65 lines containing: `ToolCall` struct, `extractKeyInput()`, `unquoteField()`
- These are consumed by `event_tools.go` (ToolCallsFromEvents), `logfmt.go` (FormatEvent), `status.go` (AgentDetail)
- No JSONL-related code remains in the file
- Flagged by 6/8 review agents (code-reviewer, simplicity, pattern-recognition, architecture, tigerstyle, git-history)

## Proposed Solutions

### Option 1: Rename to tool_call.go (Recommended)

**Approach:** `git mv internal/daemon/jsonl.go internal/daemon/tool_call.go`

**Pros:** Minimal change, file name matches content, discoverable
**Cons:** None
**Effort:** 5 minutes
**Risk:** Low

### Option 2: Move contents into event_tools.go, delete jsonl.go

**Approach:** Merge `ToolCall`, `extractKeyInput`, `unquoteField` into `event_tools.go` (primary consumer)

**Pros:** Eliminates one file, better locality
**Cons:** `event_tools.go` grows from 109 to ~170 lines
**Effort:** 10 minutes
**Risk:** Low

## Recommended Action



## Acceptance Criteria

- [ ] `jsonl.go` no longer exists or is renamed
- [ ] All imports/references updated
- [ ] `go build ./...` passes
- [ ] `go test ./...` passes

## Work Log

### 2026-02-19 - Code Review Discovery

**By:** Claude Code (multi-agent review of f644f5f)

**Actions:**
- Identified naming inconsistency after dead JSONL code removal
- Confirmed no JSONL-related code remains in the file
- Verified all consumers via grep

**Learnings:**
- File names should be updated when their content changes significantly during refactors
