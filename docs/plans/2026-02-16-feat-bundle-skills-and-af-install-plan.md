---
title: "feat: Bundle skills/agents into repo and add af install command"
type: feat
date: 2026-02-16
tasks: [ts-583fa1, ts-ea281b]
---

# Bundle skills/agents into repo and add af install command

## Overview

Aetherflow's worker prompt references two skills (`review-auto`, `compound-auto`) that spawn 8 agent definitions. Today these only exist in the user's `~/.config/opencode/` directories — a fresh clone doesn't work without them. This plan bundles the minimal set of skills and agents into the aetherflow repo and adds an `af install` command that copies them to the right places.

## Problem Statement

A fresh `git clone` + `go build` of aetherflow produces a binary that spawns agents, but those agents fail at the `review` and `land` steps because the skills and agents they reference don't exist on the machine. There's no installation step, no documentation of what's needed, and no way to know what's missing until an agent errors out mid-session.

The bootstrapping gap:
1. Worker prompt says `Load skill: review-auto` → opencode looks in `~/.config/opencode/skills/review-auto/SKILL.md` → file not found → agent fails
2. review-auto says `Task(subagent_type="code-reviewer", ...)` → opencode looks in `~/.config/opencode/agents/code-reviewer.md` → file not found → subagent fails

## Proposed Solution

Two parts:

**Part 1 — Bundle files into the repo.** Copy the canonical skill and agent definitions into `skills/` and `agents/` directories in the aetherflow repo. These get embedded into the `af` binary via `//go:embed` (matching the existing `prompts_embed.go` pattern).

**Part 2 — `af install` command.** A new CLI subcommand that copies the embedded skills and agents to `~/.config/opencode/`. Shows what will be installed, asks for confirmation, reports results. Hardcoded to opencode for now — no multi-harness abstraction.

## What gets bundled

### Skills (2)

Referenced by the worker prompt (`skill: review-auto` and `skill: compound-auto`):

| Skill | Purpose | Agents spawned |
|-------|---------|----------------|
| `review-auto` | Autonomous code review — spawns parallel reviewers on the diff | code-reviewer, code-simplicity-reviewer, security-sentinel, architecture-strategist, grug-brain-reviewer, tigerstyle-reviewer |
| `compound-auto` | Knowledge compounding at task completion — captures solutions, updates feature matrix, writes handoff | None (uses built-in `general` subagent type) |

Source: `~/.config/opencode/skills/{review-auto,compound-auto}/SKILL.md` → `skills/{review-auto,compound-auto}/SKILL.md`

### Agents (8)

The 6 agents spawned by `review-auto`, plus 2 additional agents used by the interactive `workflows:review` command:

| Agent | Used by | Purpose |
|-------|---------|---------|
| `code-reviewer` | review-auto | Bugs, correctness, logic errors |
| `code-simplicity-reviewer` | review-auto | Complexity, YAGNI, simplification |
| `security-sentinel` | review-auto | Security vulnerabilities |
| `architecture-strategist` | review-auto | Architectural concerns, coupling |
| `grug-brain-reviewer` | review-auto | Overengineering, debuggability |
| `tigerstyle-reviewer` | review-auto | Safety, assertions, control flow |
| `performance-oracle` | workflows:review | Performance bottlenecks, algorithmic complexity, caching |
| `agent-native-reviewer` | workflows:review | Agent/UI action parity, context parity |

Source: `~/.config/opencode/agents/<name>.md` → `agents/<name>.md`

### Not bundled

- **Plugins** — The plugin system (ep-ca386e) is still being built. The `plugins/` directory can be added later when plugins exist.
- **Other agents** — `workflows:review` references even more agents (pattern-recognition-specialist, data-integrity-guardian, etc.) but these aren't part of the aetherflow core workflow. Users who want the full compound-engineering agent suite can install that separately.
- **Other skills** — Skills like `brainstorming`, `research`, `git-worktree`, etc. are general-purpose user tools, not aetherflow dependencies.

## Technical Approach

### Repo layout

Assets live inside the `internal/install/` package, co-located with the embed directive — matching exactly how `internal/daemon/prompts/` works with `prompts_embed.go`.

```
aetherflow/
├── internal/
│   └── install/
│       ├── install.go        # Core install logic (accepts target dir as parameter)
│       ├── install_test.go   # Tests (use t.TempDir() as target)
│       ├── assets_embed.go   # //go:embed directive
│       ├── skills/
│       │   ├── review-auto/
│       │   │   └── SKILL.md
│       │   └── compound-auto/
│       │       └── SKILL.md
│       └── agents/
│           ├── code-reviewer.md
│           ├── code-simplicity-reviewer.md
│           ├── security-sentinel.md
│           ├── architecture-strategist.md
│           ├── grug-brain-reviewer.md
│           ├── tigerstyle-reviewer.md
│           ├── performance-oracle.md
│           └── agent-native-reviewer.md
└── cmd/af/cmd/
    └── install.go            # Cobra command (resolves target dir, calls install package)
```

### Embedding strategy

Follow the existing `internal/daemon/prompts_embed.go` pattern exactly — assets co-located with the embed directive:

```go
// internal/install/assets_embed.go
package install

import "embed"

//go:embed skills agents
var assetsFS embed.FS
```

Note: no `all:` prefix — this excludes dotfiles (`.DS_Store`, etc.) which is what we want.

### Install function signature

The install logic accepts the target directory as a parameter. The Cobra command resolves the default (`~/.config/opencode/`) and passes it in. This keeps install logic harness-agnostic and makes testing trivial.

```go
// internal/install/install.go
func Install(targetDir string, opts Options) (Result, error)

// cmd/af/cmd/install.go — resolves default, allows override
targetDir := filepath.Join(homeDir, ".config", "opencode")
```

A `--target` flag on the Cobra command allows the user to override the default install location.

### Opencode detection

Before installing, `af install` checks that opencode is actually present on the machine. Two signals, either is sufficient:

1. `opencode` binary is on `$PATH` (`exec.LookPath("opencode")`)
2. `~/.config/opencode/` directory exists

If neither is found, error and refuse:

```
opencode not detected. Install opencode first:

  brew install opencode    # macOS
  curl -fsSL https://opencode.ai/install | bash  # linux

  https://opencode.ai

Or use --target to specify a custom install location.
```

The `--target` flag bypasses detection — if the user explicitly tells us where to install, we trust them.

### Install command behavior

```
$ af install

Installing aetherflow skills and agents to ~/.config/opencode/:

  Skills:
    write  review-auto/SKILL.md      → ~/.config/opencode/skills/review-auto/SKILL.md
    write  compound-auto/SKILL.md    → ~/.config/opencode/skills/compound-auto/SKILL.md

  Agents:
    write  code-reviewer.md          → ~/.config/opencode/agents/code-reviewer.md
    write  security-sentinel.md      → ~/.config/opencode/agents/security-sentinel.md
    skip   grug-brain-reviewer.md    (up to date)

Proceed? [Y/n] y

  ✓ review-auto/SKILL.md
  ✓ compound-auto/SKILL.md
  ✓ code-reviewer.md
  ✓ security-sentinel.md
  · grug-brain-reviewer.md (up to date)
  ...

Done. 8 written, 2 up to date.
```

Override the target directory with `--target`:

```
$ af install --target /custom/path --yes
```

### Per-file behavior

For each embedded file, resolve the target path and compare content using `bytes.Equal` (byte-level comparison):

| Condition | Action | Display |
|-----------|--------|---------|
| Target doesn't exist or content differs | Write | `write` |
| Target exists and content matches | Skip | `up to date` |

Create parent directories as needed with `os.MkdirAll`.

### Flags

| Flag | Behavior |
|------|----------|
| `--dry-run` | Show what would happen, don't write files. No confirmation prompt. |
| `--yes` / `-y` | Skip confirmation prompt. Write immediately. |
| `--target` | Override default install directory (default: `~/.config/opencode/`). |
| (no flags) | Show plan, prompt for confirmation, write files. |

### Error handling

- Opencode not detected (no binary on PATH, no `~/.config/opencode/`) and `--target` not set → error and exit 1
- Home directory unresolvable → error and exit 1
- Write failure on any file → log error, continue with remaining files, report failures at end, exit 1
- User declines confirmation → print "Aborted.", exit 0
- No files need changes → "Everything is up to date.", exit 0

Exit 0 on success, 1 on any failure.

## Acceptance Criteria

### Part 1: Bundle (ts-583fa1)

- [x] `internal/install/skills/review-auto/SKILL.md` exists with content matching the canonical version
- [x] `internal/install/skills/compound-auto/SKILL.md` exists with content matching the canonical version
- [x] `internal/install/agents/` contains all 8 agent `.md` files with content matching canonical versions
- [x] Files are embedded into the binary via `//go:embed` in `internal/install/assets_embed.go`
- [x] `go build ./cmd/af` succeeds with the embedded assets

### Part 2: Install command (ts-ea281b, scoped to MVP)

- [x] `af install` shows a plan of what will be written and prompts for confirmation
- [x] `af install --yes` installs without prompting
- [x] `af install --dry-run` shows what would happen without writing
- [x] `af install --target /path` overrides the default install directory and bypasses detection
- [x] Default target is `~/.config/opencode/`
- [x] Opencode detection: refuses to install if opencode not found (no binary on PATH, no config dir), unless `--target` is set
- [x] Files are copied to `<target>/skills/<name>/SKILL.md` and `<target>/agents/<name>.md`
- [x] Install creates parent directories if they don't exist
- [x] Idempotent: running twice produces the same result (second run shows "up to date" for all files)
- [x] Exit code 0 on success, 1 on write failure
- [x] Command registered in cobra tree, appears in `af --help`
- [x] Integration test verifies install to `t.TempDir()` (fresh, idempotent, update scenarios)

## Dependencies

- No external dependencies. The harness abstraction (ts-555e54) is nice-to-have but not required — this plan hardcodes opencode paths. The harness can be swapped in later if/when multi-harness support is needed.

## What this plan does NOT cover

- Multi-harness support (claude, codex) — future work, potentially via the harness abstraction
- Plugin installation — plugins don't exist yet (ep-ca386e)
- Manifest file — not needed for 2 skills + 8 agents. If the set grows significantly, a manifest can be added later.
- `af uninstall` — out of scope
- Version tracking in installed files — content comparison handles staleness implicitly
- Interactive harness picker — hardcoded to opencode

## References

### Internal

- `internal/daemon/prompts_embed.go` — existing `//go:embed` pattern
- `internal/daemon/prompt.go:64` — `RenderPrompt()` shows embed FS usage
- `cmd/af/cmd/daemon.go` — Cobra command registration pattern
- `cmd/af/cmd/root.go` — Root command and persistent flags
- `internal/daemon/prompts/worker.md:110` — `skill: review-auto` reference
- `internal/daemon/prompts/worker.md:128` — `skill: compound-auto` reference
- `docs/solutions/security-issues/path-traversal-validation-pattern-20260210.md` — path validation pattern (applies if component names ever come from user input)

### Source files

- `~/.config/opencode/skills/review-auto/SKILL.md` — canonical review-auto skill
- `~/.config/opencode/skills/compound-auto/SKILL.md` — canonical compound-auto skill
- `~/.config/opencode/agents/*.md` — canonical agent definitions

### Related tasks

- `ts-583fa1` — Bundle skills into the aetherflow repo (this plan, Part 1)
- `ts-ea281b` — Implement af install command (this plan, Part 2 — scoped to MVP)
- `ts-555e54` — Harness abstraction (completed on branch, not required for this plan)
- `ep-ceb163` — Parent epic: Skill installation
- `ep-ca386e` — Plugin epic (future — plugins not bundled yet)
