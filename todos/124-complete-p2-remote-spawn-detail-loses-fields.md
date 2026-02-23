---
status: pending
priority: p2
issue_id: "124"
tags: [code-review, agent-native, api-design]
dependencies: []
---

# Remote spawn detail endpoint loses typed fields (Provider, State, ServerRef)

## Problem Statement

When an agent queries `af status <remote-spawn-id> --json`, the response is an `AgentDetail` embedding `AgentStatus`. For remote spawns, `buildRemoteSpawnDetail` packs `Provider` and `State` into `TaskTitle` as a formatted string (`"[sprites] running"`), losing them as discrete JSON fields. The detail endpoint is strictly less informative than the list endpoint for remote spawns.

An agent wanting to act on a specific remote spawn's provider, state, or sandbox ID must either parse `TaskTitle` with string manipulation or fetch the full status list and filter.

## Findings

- `internal/daemon/status.go:355` — `TaskTitle: fmt.Sprintf("[%s] %s", rec.Provider, rec.State)`
- Detail response loses: `Provider`, `State`, `ProviderSandboxID`, `ServerRef`, `UpdatedAt`
- List response (`RemoteSpawnStatus` in `FullStatus.remote_spawns`) has all fields as discrete JSON keys
- `LastError` is available via `errors[]` array but mixed with other error types
- Flagged by: agent-native-reviewer

## Proposed Solutions

### Option 1: Add optional RemoteSpawn field to AgentDetail

**Approach:** Add `RemoteSpawn *RemoteSpawnStatus` to `AgentDetail`, populated only for remote spawns.

```go
type AgentDetail struct {
    AgentStatus
    RemoteSpawn *RemoteSpawnStatus `json:"remote_spawn,omitempty"`
    ToolCalls   []ToolCall         `json:"tool_calls"`
    Errors      []string           `json:"errors,omitempty"`
}
```

**Pros:**
- Full field parity with list endpoint
- `omitempty` keeps it clean for non-remote agents
- No breaking changes

**Cons:**
- `AgentDetail` grows in scope
- Client-side `AgentDetail` also needs the field

**Effort:** 30 minutes
**Risk:** Low

---

### Option 2: Populate existing AgentStatus fields more carefully

**Approach:** Set `TaskTitle` to just the state, use `LastLog` for the provider. Less clean but no struct changes.

**Effort:** 10 minutes
**Risk:** Medium — overloads field semantics

## Recommended Action

## Technical Details

**Affected files:**
- `internal/daemon/status.go:351-378` — `buildRemoteSpawnDetail`
- `internal/daemon/status.go:217-221` — `AgentDetail` struct
- `internal/client/client.go:222-226` — client `AgentDetail` struct

## Acceptance Criteria

- [ ] `af status <remote-spawn-id> --json` returns `provider`, `state`, `provider_sandbox_id`, `server_ref` as discrete fields
- [ ] Non-remote agent detail responses unchanged
- [ ] Tests cover the new field

## Work Log

### 2026-02-22 - Code Review Round 5

**By:** Claude Code

**Actions:**
- Agent-native reviewer identified field loss in detail endpoint
