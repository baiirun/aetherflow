---
status: pending
priority: p3
issue_id: "080"
tags: [code-review, housekeeping]
dependencies: []
---

# Remove stray photography research doc from PR

## Problem Statement

`docs/research/2026-02-20-topic-synthesis-why-people-shoot-black-and-white-vs-color.md` (79 lines) is unrelated to this PR. Committed accidentally in the HTTP transport migration commit.

## Findings

- The file is a photography research document that has no relation to the HTTP transport migration work.
- It was included accidentally in a commit that should only contain transport-related changes.
- Its presence adds noise to the PR diff and makes review harder.

## Proposed Solution

Remove from this branch or move to a separate commit.

## Acceptance Criteria

- [ ] The photography research doc is no longer part of this PR's diff.
- [ ] If the doc is needed, it exists in a separate branch/commit with appropriate context.

## Work Log

- **Effort estimate:** Small (5 min)
