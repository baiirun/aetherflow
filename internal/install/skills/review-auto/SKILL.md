---
name: review-auto
description: Autonomous code review using parallel subagent reviewers. No user interaction. Spawns fresh-eyes reviewers on your diff, collects findings, returns prioritized results. Use mid-session after implementing code.
---

# Autonomous Review

Run parallel code reviewers on your current changes. Each reviewer gets a fresh context — no sunk cost in your implementation.

## Constraints

- **No user interaction.** Do not ask questions. Do not wait for approval.
- **Review what's on the branch.** Don't checkout other branches or use worktrees.
- **Return findings, don't fix.** Your job is to identify issues, not resolve them. The caller fixes.

## Steps

### 1. Gather context

Get the list of changed files and a brief summary of what this change does:

```bash
git diff --stat $(git merge-base HEAD main)..HEAD
```

You do NOT need to fetch the full diff yourself. Each reviewer will fetch it independently.

### 2. Spawn reviewers in parallel

Launch these Task agents **simultaneously** — they are independent and should all run in parallel.

**CRITICAL**: Do NOT paste the diff into the prompt. Each subagent runs in the same working directory and can fetch the diff itself. Tell each reviewer to run `git diff $(git merge-base HEAD main)..HEAD` to get the diff. This avoids truncation on large diffs and prevents lazy placeholders like "[Same diff]" that break subagents (each subagent has fresh context and cannot see other prompts).

Include in each reviewer prompt:
- The task context: what this change is trying to accomplish (1-2 sentences)
- The list of changed files (from `git diff --stat`)
- The instruction to run `git diff $(git merge-base HEAD main)..HEAD` to read the full diff
- The instruction to return findings as a prioritized list with P1/P2/P3 severity

```
Task(subagent_type="code-reviewer", prompt="Review code changes for bugs, correctness, and logic errors.

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="code-simplicity-reviewer", prompt="Review code changes for unnecessary complexity and simplification opportunities.

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="security-sentinel", prompt="Review code changes for security vulnerabilities.

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="architecture-strategist", prompt="Review code changes for architectural concerns.

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="grug-brain-reviewer", prompt="Review code changes for overengineering and debuggability.

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="tigerstyle-reviewer", prompt="Review code changes for safety, assertion density, explicit limits, and control flow.

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")
```

### 3. Collect and deduplicate findings

Merge findings from all reviewers. Deduplicate — multiple reviewers may flag the same issue. Keep the highest severity when duplicates differ.

Discard any reviewer that returned an error, asked for clarification instead of reviewing, or returned empty results. These are failed reviews — do not retry, just skip them.

### 4. Return prioritized findings

Return findings to the caller in this format:

```
## Review Findings

### P1 — Must fix (bugs, correctness, security)
- [finding]

### P2 — Should fix (simplicity, architecture, maintainability)
- [finding]

### P3 — Consider (style, minor improvements)
- [finding]

### Clean
No findings at this severity level.
```

If there are no findings at any level, return "Review clean — no findings."
