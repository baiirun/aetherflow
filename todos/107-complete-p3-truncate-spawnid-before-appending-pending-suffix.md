---
status: pending
priority: p3
issue_id: "107"
tags: [code-review, quality, display]
dependencies: []
---

# Truncate SpawnID before appending (pending) suffix

## Problem Statement

When displaying a spawn-only entry, the code appends ` (pending)` to the SpawnID before truncation:

```go
displayID = e.SpawnID + " (pending)"
```

A long SpawnID could be truncated mid-suffix by `truncate(displayID, 34)`, producing something like `spawn-very_long_name-abc (pen…`. It would be cleaner to truncate the SpawnID first, then append the suffix.

## Findings

- `cmd/af/cmd/sessions.go:167-168` — suffix appended before truncation
- Typical spawn IDs are ~24 chars; adding ` (pending)` makes 34 — exactly the column width
- Longer IDs would produce ugly truncation

## Proposed Solutions

### Option 1: Truncate SpawnID first, then append suffix

```go
const pendingSuffix = " (pending)"
maxIDLen := 34 - len(pendingSuffix)
displayID = truncate(e.SpawnID, maxIDLen) + pendingSuffix
```

**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] SpawnID truncated before suffix is appended
- [ ] Display fits within 34-char column
- [ ] `go build ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code
