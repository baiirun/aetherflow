---
status: pending
priority: p3
issue_id: "082"
tags: [code-review, architecture]
dependencies: []
---

# Move state predicates to daemon package

## Problem Statement

`isRemoteSpawnPending` and `isRemoteSpawnTerminal` in `cmd/af/cmd/sessions.go:441-447` operate on daemon types but live in the CLI package. They should be methods on `RemoteSpawnState` or exported functions in the daemon package, co-located with the type definition. Also inconsistent signature with `isTerminalRemoteSpawnState` in the daemon package which takes `RemoteSpawnState` directly.

## Findings

- `isRemoteSpawnPending` and `isRemoteSpawnTerminal` are defined in the CLI layer but operate purely on daemon-owned types.
- This violates locality of behavior — understanding `RemoteSpawnState` transitions requires looking in two packages.
- The daemon package already has `isTerminalRemoteSpawnState` which takes `RemoteSpawnState` directly, but the CLI versions take `*RemoteSpawn` and extract the state internally, creating an inconsistent API surface.

## Proposed Solution

Move to daemon package, take `RemoteSpawnState` param, export as `IsRemoteSpawnPending`/`IsRemoteSpawnTerminal`.

## Acceptance Criteria

- [ ] `isRemoteSpawnPending` and `isRemoteSpawnTerminal` are removed from `cmd/af/cmd/sessions.go`.
- [ ] Exported `IsRemoteSpawnPending(RemoteSpawnState)` and `IsRemoteSpawnTerminal(RemoteSpawnState)` exist in the daemon package.
- [ ] Signatures are consistent with the existing `isTerminalRemoteSpawnState` pattern (taking `RemoteSpawnState` directly).
- [ ] All callers in the CLI package are updated to use the daemon package functions.
- [ ] Tests pass.

## Work Log

- **Effort estimate:** Small (15 min)
