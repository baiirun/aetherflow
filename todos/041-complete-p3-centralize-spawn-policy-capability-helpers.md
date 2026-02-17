---
status: complete
priority: p3
issue_id: "041"
tags: [code-review, architecture, maintainability, daemon]
dependencies: []
---

# Centralize spawn policy capability checks

## Problem Statement

Spawn-policy behavior checks are spread across daemon runtime, status enrichment, CLI status rendering, and TUI rendering, including string literals in some layers. This can drift over time.

## Findings

- Policy checks appear in `internal/daemon/daemon.go`, `internal/daemon/status.go`, `cmd/af/cmd/status.go`, and `internal/tui/tui.go`.
- `cmd/af/cmd/status.go:188` and `internal/tui/tui.go:378` compare string literals.
- Behavior is currently correct, but central helpers would reduce future inconsistency.

## Proposed Solutions

### Option 1: Add helper methods for policy capabilities

**Approach:** Introduce helpers like `AutoSchedulingEnabled()` / `ProgEnrichmentEnabled()` and reuse across layers.

**Pros:**
- Reduces semantic drift.
- Improves readability at call sites.

**Cons:**
- Small refactor touching multiple files.

**Effort:** Medium

**Risk:** Low

---

### Option 2: Keep current logic and add comment contracts/tests

**Approach:** Leave code shape intact but document policy matrix and add more cross-layer tests.

**Pros:**
- Lowest code churn.

**Cons:**
- Drift risk remains.

**Effort:** Small

**Risk:** Medium

## Recommended Action

Implemented Option 1 with shared policy helpers in daemon and client layers.

## Technical Details

- Affected files: `internal/daemon/daemon.go`, `internal/daemon/status.go`, `cmd/af/cmd/status.go`, `internal/tui/tui.go`
- Components: policy semantics and UI exposure
- Database changes: No

## Resources

- Review target: commit `006131715aed57dedad6bda8871350e5401f8816`

## Acceptance Criteria

- [x] Policy checks use shared constants/helpers rather than ad-hoc string comparisons.
- [x] Status/CLI/TUI remain behaviorally consistent across policies.
- [x] Existing daemon/CLI/TUI tests pass with centralized helpers.

## Work Log

### 2026-02-16 - Initial review finding

**By:** Claude Code

**Actions:**
- Consolidated maintainability findings from architecture/pattern reviews.

**Learnings:**
- New policy axis is high-leverage; centralization reduces future churn.

### 2026-02-16 - Resolved

**By:** Claude Code

**Actions:**
- Added daemon-level spawn policy helper methods (`Normalized`, `AutoSchedulingEnabled`, `ProgEnrichmentEnabled`).
- Updated daemon runtime and status builder to use helper methods instead of ad-hoc policy checks.
- Added client-level policy constants/helpers on `FullStatus` and switched CLI/TUI rendering to use them.

**Learnings:**
- Small helper methods removed string-literal drift while keeping behavior local and readable.
