---
status: pending
priority: p3
issue_id: "007"
tags: [code-review, architecture, cleanup]
dependencies: []
---

# Deprecate old `status` RPC in favor of `status.full`

## Problem Statement

The daemon now has two status methods: the old `status` (returns untyped `map[string]any`) and the new `status.full` (returns typed `FullStatus` with enriched data). The old method is only used by `af daemon` bare command. Having two status methods with different schemas is a maintenance trap — every future status field addition requires deciding which method gets it.

## Findings

- Old `handleStatus` returns `map[string]any` (daemon.go:174)
- New `handleStatusFull` returns typed `FullStatus` (daemon.go:165)
- Old `status` used only by `af daemon` bare command (daemon.go cmd:20-21)
- `status.full` is a strict superset of the information
- Identified by: architecture-strategist

## Proposed Solutions

### Option 1: Replace `af daemon` usage with StatusFull, remove old `status` RPC

**Effort:** 30 minutes
**Risk:** Low (no external consumers)

## Acceptance Criteria

- [ ] `af daemon` bare command uses `StatusFull()` instead of `Status()`
- [ ] Old `status` RPC method removed from daemon
- [ ] Old `Status()` method removed from client
- [ ] Tests updated

## Work Log

### 2026-02-07 - Architecture Review

**By:** Claude Code
