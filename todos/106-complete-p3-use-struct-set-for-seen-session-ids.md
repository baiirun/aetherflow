---
status: pending
priority: p3
issue_id: "106"
tags: [code-review, quality, go-idioms]
dependencies: []
---

# Use map[string]struct{} for seenSessionIDs set

## Problem Statement

`buildSessionListEntries` uses `map[string]bool` for the `seenSessionIDs` set. Idiomatic Go uses `map[string]struct{}` for sets — it avoids the false-vs-absent ambiguity and saves a byte per entry.

## Findings

- `cmd/af/cmd/sessions.go:187` — `make(map[string]bool, len(recs))`
- Minor idiom issue; functionally identical since the code only checks presence

## Proposed Solutions

### Option 1: Switch to map[string]struct{} (Recommended)

```go
seenSessionIDs := make(map[string]struct{}, len(recs))
// Insert: seenSessionIDs[r.SessionID] = struct{}{}
// Check:  _, seen := seenSessionIDs[rs.SessionID]; if seen { continue }
```

**Effort:** 5 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `seenSessionIDs` uses `map[string]struct{}`
- [ ] `go test ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code
