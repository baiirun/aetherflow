---
status: pending
priority: p2
issue_id: "104"
tags: [code-review, security, ssrf]
dependencies: []
---

# Skip semantic enrichment for non-localhost ServerRef values

## Problem Statement

`fetchSessionObjective` makes HTTP GET requests to `serverRef + "/session/" + sessionID + "/message"`. With remote spawn records now merged into the session listing, `ServerRef` values from the Sprites API (e.g., `https://agent-a.sprites.app`) could be targeted by these requests.

While remote-spawn-only entries have empty SessionIDs and will be skipped, if a remote spawn transitions to having a SessionID, the enrichment would make HTTP requests to external hosts. If `remote_spawns.json` were tampered with, this becomes an SSRF surface.

## Findings

- `cmd/af/cmd/sessions.go:338-373` — `fetchSessionObjective` makes HTTP requests to `ServerRef`
- `cmd/af/cmd/sessions.go:302-318` — `loadSessionSemanticIndex` iterates all records
- Validation happens at write time (in `spawn.go`), not at read time
- The opencode message API is only available on localhost instances, not remote Sprites endpoints

## Proposed Solutions

### Option 1: Skip enrichment for non-localhost ServerRef (Recommended)

**Approach:** In `loadSessionSemanticIndex`, skip records where `ServerRef` is not localhost.

```go
if r.ServerRef == "" || r.SessionID == "" {
    continue
}
u, err := url.Parse(r.ServerRef)
if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
    continue
}
```

**Pros:** Eliminates SSRF surface; only enriches local sessions (which is the only place the API exists)
**Cons:** None — remote sessions don't expose the message API anyway
**Effort:** 10 minutes
**Risk:** Low

## Acceptance Criteria

- [ ] `loadSessionSemanticIndex` skips non-localhost `ServerRef` values
- [ ] Remote spawn entries still appear in listing (just without enrichment)
- [ ] `go test ./...` passes

## Work Log

### 2026-02-22 - Code Review Discovery

**By:** Claude Code

**Actions:**
- Identified by security-sentinel agent
- The opencode message API only runs on localhost, so enriching remote hosts is both a security risk and functionally useless
