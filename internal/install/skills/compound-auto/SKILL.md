---
name: compound-auto
description: Autonomous knowledge compounding at task completion. Captures problem solutions as structured documentation, updates feature matrix, logs learnings, and writes handoff. Use after verification passes and before creating a PR.
---

# Compound

Capture and persist everything this task produced beyond the code itself. This runs after your work passes verification and before you create a PR.

## Constraints

- **No user interaction.** Do not ask questions or wait for approval. Make reasonable decisions and move on.
- **Be honest about what you learned.** If nothing was surprising, don't fabricate learnings.
- **Preserve original context in handoffs.** The handoff replaces the task description — include any important original context so it isn't lost.

## Steps

### 1. Analyze context

Extract from your work session:

- **Problem type**: what kind of work was this (new feature, bug fix, refactor, performance, etc.)
- **Component/module**: which part of the system was affected
- **Key symptoms**: if this was a fix, what was the observable behavior
- **Investigation attempts**: what was tried and didn't work, and why
- **Root cause**: if applicable, what was actually wrong
- **Solution**: what you did and why this approach

### 2. Extract and document solution

If this task involved solving a non-trivial problem (multiple investigation attempts, non-obvious solution, future agents would benefit from knowing):

Launch parallel subagents to build structured documentation:

1. **Solution Extractor** — analyze investigation steps, identify root cause, extract working solution with code examples
2. **Related Docs Finder** — search `docs/solutions/` for related documentation, identify cross-references
3. **Prevention Strategist** — develop prevention strategies, best practices, test cases if applicable

Assemble into a structured doc at `docs/solutions/<category>/<slug>.md` with YAML frontmatter for searchability:

```yaml
---
module: <component name>
date: <YYYY-MM-DD>
problem_type: <type>
symptoms:
  - "<observable behavior>"
root_cause: <brief technical cause>
severity: <critical|high|medium|low>
tags: [<relevant>, <tags>]
---
```

**Skip this step for routine work** — simple features, straightforward implementations, no debugging involved.

### 3. Update feature matrix

If `MATRIX.md` exists in the repo, update coverage for behaviors you implemented:
- For each behavior in your DoD, add or update the corresponding row
- Set the verification command (the test that covers this behavior)
- Set coverage status and last verified date

If `MATRIX.md` doesn't exist, skip this step.

### 4. Log learnings

Did you discover anything that would help future agents working in this area?

Only log genuine insights — patterns that were non-obvious, pitfalls you hit, decisions with non-obvious tradeoffs, workarounds for external constraints.

Append learnings to `docs/solutions/learnings.md` — one entry per learning, with a one-line summary and a detail block. Create the file if it doesn't exist.

```markdown
### <concept>: <one-line summary>

<full explanation — what you learned, why it matters, and how to apply it>
```

Good learnings:
- `database: Schema migrations need built binary` — "go run doesn't embed assets; must use go build first"
- `tui: TUI has paired format functions` — "formatStatus and formatPriority must be updated together"

Not learnings (too vague, no insight):
- "Fixed the auth bug"
- "This file handles authentication"

If nothing was surprising or non-obvious, skip this step. Don't generate filler.

### 5. Write handoff

Summarize for the next agent picking up this work or the reviewer reading your PR:

- What was done and why
- Which files were modified and their roles
- What was tried and didn't work (and why) — this is the most valuable part
- Key decisions made and their reasoning
- Any remaining concerns or known limitations

Write the handoff to `docs/solutions/handoffs/<slug>-<YYYYMMDD>.md`. Create the directory if needed. Include any important original context (the DoD, key constraints) so it isn't lost — this file is the primary record of the work.
