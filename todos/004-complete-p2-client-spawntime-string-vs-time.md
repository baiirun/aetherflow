---
status: pending
priority: p2
issue_id: "004"
tags: [code-review, quality, type-safety]
dependencies: []
---

# Client AgentStatus.SpawnTime should be time.Time, not string

## Problem Statement

The daemon's `AgentStatus.SpawnTime` is `time.Time`, but the client's `AgentStatus.SpawnTime` is `string`. JSON round-trips `time.Time` as RFC3339Nano, which the client receives as a string. Then `formatUptime` parses it back with two fallback formats. This is fragile — if the daemon's serialization changes, the client breaks silently at runtime.

As a secondary issue, `formatUptime` tries both `time.RFC3339Nano` and `time.RFC3339`, but `RFC3339Nano` is a superset that parses both formats. The second parse is dead code.

## Findings

- `daemon.AgentStatus.SpawnTime` is `time.Time` (status.go:28)
- `client.AgentStatus.SpawnTime` is `string` (client.go:92)
- `formatUptime` parses string → time.Time → duration (status.go:102-110)
- `time.Parse(time.RFC3339Nano, ...)` successfully parses RFC3339 strings without nanos
- Identified by: code-reviewer, simplicity-reviewer

**Affected files:**
- `internal/client/client.go:92` — SpawnTime type
- `cmd/af/cmd/status.go:102-110` — formatUptime signature and parsing

## Proposed Solutions

### Option 1: Use time.Time in client, simplify formatUptime (Recommended)

**Approach:** Change `client.AgentStatus.SpawnTime` to `time.Time`. Go's `json.Unmarshal` handles RFC3339 into `time.Time` natively. Simplify `formatUptime` to take `time.Time` directly.

```go
// client/client.go
type AgentStatus struct {
    // ...
    SpawnTime time.Time `json:"spawn_time"`
}

// cmd/af/cmd/status.go
func formatUptime(spawnTime time.Time) string {
    d := time.Since(spawnTime)
    // ... same switch logic, no parsing needed
}
```

**Pros:**
- Compile-time type safety
- Eliminates dead code (double parse)
- Simpler tests (pass time.Time directly)

**Cons:**
- Client now imports `time` (already does for other uses)

**Effort:** 20 minutes (includes test updates)
**Risk:** Low

## Recommended Action

Option 1. Also simplifies `TestFormatUptime` — no more formatting to RFC3339 strings.

## Acceptance Criteria

- [ ] `client.AgentStatus.SpawnTime` is `time.Time`
- [ ] `formatUptime` takes `time.Time` parameter
- [ ] Dead `RFC3339` fallback parse removed
- [ ] Tests updated and passing

## Work Log

### 2026-02-07 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified type divergence and dead code during code review
- Confirmed by code-reviewer and simplicity-reviewer
