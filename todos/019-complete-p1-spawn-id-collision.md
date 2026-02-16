---
status: complete
priority: p1
issue_id: "019"
tags: [code-review, data-integrity, safety]
dependencies: []
---

# Add random suffix to spawn IDs to prevent collision

## Problem Statement

`af spawn` generates IDs as `"spawn-" + protocol.NewAgentID()` where `NewAgentID` picks from ~120 adjectives × ~120 nouns = ~14,400 combinations. The pool's `NameGenerator` has collision detection (retry loop + used-names map), but spawn IDs bypass it entirely.

If two concurrent spawns collide:
- Worktree/branch conflicts (`af/spawn-ghost_wolf` already exists)
- Log file clobbering (both write to same `.jsonl`)
- Registry overwrite (second silently replaces first — tested in `TestSpawnRegistryRegisterOverwrites`)

Birthday paradox: ~50% collision probability at ~120 concurrent spawns.

## Findings

- Found by: data-integrity-guardian, security-sentinel, tigerstyle-reviewer
- Location: `cmd/af/cmd/spawn.go:82`
- `Register` silently overwrites duplicates (spawn_registry.go:41)
- Pool uses `NameGenerator` with collision detection (protocol/names.go:136-151)

## Proposed Solutions

### Option 1: Add random hex suffix (Recommended)

**Approach:** Append a 4-char hex suffix: `spawn-ghost_wolf-a3f2`

```go
import "crypto/rand"

func newSpawnID() string {
    name := protocol.GenerateAgentName()
    suffix := make([]byte, 2)
    _, _ = rand.Read(suffix)
    return fmt.Sprintf("spawn-%s-%x", name, suffix)
}
```

~14,400 × 65,536 = ~943 million combinations. Collision is negligible.

- **Pros:** One-line fix, massive namespace expansion, no daemon dependency
- **Cons:** Slightly longer IDs
- **Effort:** Small
- **Risk:** Low

### Option 2: Use daemon's NameGenerator via RPC

**Approach:** Add a `spawn.reserve_id` RPC that allocates a unique ID from the daemon's NameGenerator.

- **Pros:** Guaranteed uniqueness (tracked set)
- **Cons:** Requires running daemon; adds complexity; breaks daemon independence
- **Effort:** Medium
- **Risk:** Medium — couples spawn to daemon

### Option 3: Check for existing worktree/branch before proceeding

**Approach:** Before creating the worktree, check if the branch exists and retry with a new ID.

- **Pros:** Catches collisions at point of use
- **Cons:** Doesn't prevent registry collision; adds retry logic
- **Effort:** Small
- **Risk:** Low

## Recommended Action

Option 1 — random hex suffix. Maximum impact, minimum effort, no dependencies.

## Technical Details

- **Affected files:** `cmd/af/cmd/spawn.go` (line 82)
- **Components:** spawn ID generation

## Acceptance Criteria

- [ ] Spawn IDs include random suffix (e.g., `spawn-ghost_wolf-a3f2`)
- [ ] Collision probability < 1 in 1 million for 1000 concurrent spawns

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | Found by 3 reviewers — strong consensus |

## Resources

- Birthday paradox calculator: n²/2m where n=spawns, m=namespace size
- Existing: `protocol.NameGenerator.Generate()` has collision retry loop
