---
status: complete
priority: p3
issue_id: "048"
tags: [code-review, documentation, cleanup]
dependencies: []
---

# Sweep stale JSONL references from comments and docs

## Problem Statement

After removing JSONL reading code, ~15 stale references to "JSONL" remain in comments, help text, and documentation. These are misleading to developers and users.

## Findings

User-facing help text:
- `cmd/af/cmd/status.go:27` — "parsed from the agent's JSONL log"
- `cmd/af/cmd/tui.go:19` — "Log stream: full-screen JSONL log tail"
- `cmd/af/cmd/spawn.go:52` — "Directory for agent JSONL log files"

Internal comments:
- `pool.go:69,85` — ProcessStarter/ExecProcessStarter docs reference JSONL
- `pool.go:108,117,139` — logFilePath, openLogFile, Pool.logDir comments
- `config.go:108-109` — LogDir doc references JSONL

Documentation:
- `README.md` — ~6 JSONL references
- `MATRIX.md:18` — references deleted TestParseSessionID

## Proposed Solutions

### Option 1: Batch comment sweep

**Approach:** Replace "JSONL log" with "log" or "event stream" across all affected files.

**Pros:** Complete, prevents future confusion
**Cons:** Touches many files for comments only
**Effort:** 15 minutes
**Risk:** Low

## Recommended Action



## Acceptance Criteria

- [ ] No user-facing help text references JSONL
- [ ] MATRIX.md doesn't reference deleted tests
- [ ] Internal comments updated or marked for future cleanup

## Work Log

### 2026-02-19 - Code Review Discovery

**By:** Claude Code (pattern-recognition-specialist)
