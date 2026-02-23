---
status: pending
priority: p2
issue_id: "111"
tags: [code-review, testing, architecture]
dependencies: []
---

# Add wire contract test for RemoteSpawnStatus daemon↔client boundary

## Problem Statement

`RemoteSpawnStatus` is defined independently in both `internal/daemon/status.go` and `internal/client/client.go` with matching JSON tags but different Go types (`RemoteSpawnState` vs `string`). If someone adds or renames a field on one side, the JSON contract silently diverges with no test catching it.

This was flagged by 3 review agents (architecture-strategist, tigerstyle-reviewer, code-reviewer) as the highest-risk finding in Phase 5.

## Findings

- `daemon.RemoteSpawnStatus` at `internal/daemon/status.go:29-39` uses `RemoteSpawnState` typed string for State
- `client.RemoteSpawnStatus` at `internal/client/client.go:180-190` uses plain `string` for State
- The existing `SpawnStatus` pattern has the same risk but mitigates it with direct type conversion (`SpawnStatus(e)`)
- `RemoteSpawnStatus` uses field-by-field mapping (status.go:189-201), so the compiler doesn't catch structural drift
- State constants are also duplicated: daemon `RemoteSpawnRequested`, etc. vs client `RemoteSpawnRequested`, etc.

## Proposed Solutions

### Option 1: JSON round-trip test in status_test.go

**Approach:** Marshal a `daemon.RemoteSpawnStatus`, unmarshal into raw map, and assert expected JSON keys. Also assert state constant parity.

**Pros:**
- Catches field drift at CI time
- No cross-package import needed (test stays in daemon package)

**Cons:**
- Tests JSON keys, not the client type directly (can't import client from daemon test)

**Effort:** 30 minutes

**Risk:** Low

---

### Option 2: Integration test in a separate test package

**Approach:** Create a test in `internal/daemon/status_contract_test.go` (or a new `internal/wirecompat` package) that imports both daemon and client, marshals daemon type, unmarshals into client type, and asserts all fields survive the round-trip. Also add constant parity assertions.

**Pros:**
- True end-to-end contract test
- Catches both field and constant drift

**Cons:**
- Requires either a new package or careful import management
- Slightly more setup

**Effort:** 1 hour

**Risk:** Low

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status.go:29-39` — daemon RemoteSpawnStatus
- `internal/client/client.go:180-190` — client RemoteSpawnStatus
- `internal/client/client.go:134-140` — client state constants
- `internal/daemon/remote_spawn_store.go:39-45` — daemon state constants
- New test file for contract assertions

## Acceptance Criteria

- [ ] Test marshals `daemon.RemoteSpawnStatus` with all fields populated
- [ ] Test asserts all expected JSON keys are present in the output
- [ ] Test asserts state constant string values match between daemon and client
- [ ] Test fails if a field is added to one side but not the other
- [ ] Tests pass

## Work Log

### 2026-02-22 - Initial Discovery

**By:** Claude Code (code review)

**Actions:**
- Identified wire contract gap across 3 independent review agents
- Verified existing `SpawnStatus` has same pattern but uses type conversion
- Confirmed no existing test covers this boundary

**Learnings:**
- The daemon→client type boundary is the most dangerous seam in this architecture
- Field-by-field mapping (unlike type conversion) doesn't get compiler safety
