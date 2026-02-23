---
status: pending
priority: p3
issue_id: "115"
tags: [code-review, security, display]
dependencies: []
---

# Sanitize LastError before persisting and exposing over wire

## Problem Statement

When the Sprites API returns a non-2xx response, the raw response body is embedded in the error message (`sprites_client.go:72`: `body=%q`) and stored verbatim as `LastError` in the `RemoteSpawnRecord`. This `LastError` is then exposed:
1. In `af status --json` output (no sanitization)
2. In CLI display (truncated + stripANSI, partially mitigated)
3. Persisted to `remote_spawns.json` on disk

Provider API error responses could contain internal URLs, request IDs, or reflected debug info.

Flagged by security-sentinel. This is a pre-existing design issue in the spawn command, not introduced by the Phase 5 diff, but exposed more broadly by the status display.

## Findings

- `internal/daemon/sprites_client.go:72` — raw response body embedded in error
- `cmd/af/cmd/spawn.go:204` — `rec.LastError = err.Error()` stored verbatim
- `internal/daemon/status.go:198` — mapped verbatim to wire type
- `cmd/af/cmd/status.go:76-80` — JSON output path has no truncation

## Proposed Solutions

### Option 1: Truncate LastError at wire-type mapping

**Approach:** Truncate `LastError` to 256 chars when mapping to `RemoteSpawnStatus`.

**Pros:** Both CLI and JSON paths benefit
**Cons:** Full error still in remote_spawns.json for debugging
**Effort:** 15 minutes
**Risk:** Low

### Option 2: Sanitize at storage time

**Approach:** Strip the `body=...` portion from provider errors before persisting.

**Pros:** Prevents any exposure path
**Cons:** Loses debugging info; harder to troubleshoot provider issues
**Effort:** 30 minutes
**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status.go:198` — wire mapping
- Optionally `cmd/af/cmd/spawn.go:204` — storage time

## Acceptance Criteria

- [ ] `LastError` in wire type is bounded in length
- [ ] Raw provider response body does not appear in `af status --json` output
- [ ] Full error still available in remote_spawns.json for debugging
- [ ] Tests pass

## Work Log

### 2026-02-22 - Initial Discovery

**By:** Claude Code (code review)

**Actions:**
- Security sentinel identified potential information leakage
- Traced LastError flow from sprites_client through spawn.go to status display
- Confirmed CLI path is partially mitigated (truncate + stripANSI) but JSON path is not
