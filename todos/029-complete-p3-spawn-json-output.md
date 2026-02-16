---
status: complete
priority: p3
issue_id: "029"
tags: [code-review, agent-native]
dependencies: []
---

# Add --json flag to af spawn for machine-parseable output

## Problem Statement

An agent parsing the output of `af spawn "..." -d` to extract the spawn ID and log path must parse human-readable text. A `--json` flag would make programmatic consumption trivial.

## Findings

- Found by: agent-native-reviewer
- Location: `cmd/af/cmd/spawn.go:219-221` — detached output is human-formatted
- Other commands (e.g., `af status --json`) support JSON output

## Proposed Solutions

Add `--json` flag that outputs `{"spawn_id": "...", "pid": ..., "log_path": "..."}`.

- **Effort:** Small
- **Risk:** Low

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | Important for agent-to-agent spawning |
