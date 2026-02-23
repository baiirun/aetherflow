---
status: pending
priority: p3
issue_id: "117"
tags: [code-review, naming]
dependencies: []
---

# Rename StatusSources.RSpawns to RemoteSpawns

## Problem Statement

`StatusSources` has fields `Pool`, `Spawns`, and `RSpawns`. The abbreviation `RSpawns` is inconsistent — the other fields are unabbreviated. TigerStyle reviewer flagged this as a naming convention violation.

## Findings

- `internal/daemon/status.go:80` — `RSpawns *RemoteSpawnStore`
- `internal/daemon/daemon.go:365` — `RSpawns: d.rspawns`
- All test call sites use `RSpawns:`

## Proposed Solutions

### Option 1: Rename to RemoteSpawns

**Approach:** Simple rename of the struct field.

**Effort:** 10 minutes
**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status.go` — struct definition + function body
- `internal/daemon/daemon.go` — call site
- `internal/daemon/status_test.go` — test call sites

## Acceptance Criteria

- [ ] Field renamed to `RemoteSpawns`
- [ ] All call sites updated
- [ ] Tests pass

## Work Log

### 2026-02-22 - Initial Discovery

**By:** Claude Code (code review)

**Actions:**
- TigerStyle reviewer flagged abbreviation inconsistency
