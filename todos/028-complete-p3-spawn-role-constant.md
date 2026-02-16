---
status: complete
priority: p3
issue_id: "028"
tags: [code-review, quality]
dependencies: []
---

# Add RoleSpawn typed constant instead of raw "spawn" string

## Problem Statement

Pool agents use typed `Role` constants (`RoleWorker`, `RolePlanner`). Spawn agents use the raw string `"spawn"`. This means the role field for spawns isn't type-safe and could diverge.

## Findings

- Found by: pattern-recognition-specialist
- Location: `internal/daemon/status.go:289` — `Role: "spawn"`

## Proposed Solutions

Add `RoleSpawn Role = "spawn"` to role constants and use it in `buildSpawnDetail`.

- **Effort:** Tiny
- **Risk:** None

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | |
