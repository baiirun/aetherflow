---
status: complete
priority: p2
issue_id: "054"
tags: [code-review, architecture, cli]
dependencies: []
---

# Decouple sprites spawn path from local daemon URL validation

## Problem Statement

`af spawn --provider sprites` still executes local-only `ValidateServerURLLocal`, so remote spawn can fail due to unrelated local server config.

## Findings

- `cmd/af/cmd/spawn.go:84` runs local validation before provider branch.
- This couples remote provider mode to local daemon assumptions.

## Proposed Solutions

### Option 1: Branch first, validate per provider

**Approach:** Move provider dispatch before local validation and only validate local URL for local path.

**Pros:** Correct behavior and clearer control flow.

**Cons:** Small reordering refactor.

**Effort:** 1-2 hours

**Risk:** Low

## Recommended Action

Implement Option 1.

## Technical Details

**Affected files:**
- `cmd/af/cmd/spawn.go`

## Acceptance Criteria

- [ ] `--provider sprites` does not depend on local server-url validity
- [ ] local spawn path behavior unchanged
- [ ] tests cover both provider paths

## Work Log

### 2026-02-20 - Review Finding Captured

**By:** Claude Code

**Actions:**
- Logged branch-ordering coupling issue from code-reviewer/simplicity reviewer.
