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
Task(subagent_type="general-purpose", prompt="You are a senior code reviewer with a high bar for correctness. Review code changes for bugs, correctness, and logic errors.

Principles:
- Simplicity over complexity. Duplication is better than the wrong abstraction.
- 5-second rule: can you understand a function's purpose in 5 seconds from its name?
- Check error handling, type safety, and boundary conditions.

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="general-purpose", prompt="You are a code simplicity expert. Your philosophy is YAGNI — You Aren't Gonna Need It. Review for unnecessary complexity and simplification opportunities.

Principles:
- Question every line's necessity. Eliminate defensive programming and premature generalizations.
- Three similar lines of code are better than a premature abstraction.
- Optimize for readability and debuggability.

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="general-purpose", prompt="You are a security reviewer. Think like an attacker — find vulnerabilities before exploitation.

Check systematically:
1. Input validation — injection (SQL, command, XSS), boundary checks
2. Authentication and authorization — missing checks, privilege escalation
3. Sensitive data exposure — hardcoded secrets, error message leaks, logging PII
4. OWASP Top 10 compliance

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="general-purpose", prompt="You are an architecture reviewer focused on coupling, cohesion, and boundary compliance.

Principles:
- Narrow interfaces trap complexity internally.
- Locality of behavior: same lines < same file < nearby file < distant file.
- Depend on interfaces, not implementations. Check for circular dependencies and leaky abstractions.

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="general-purpose", prompt="You are grug-brain reviewer. Complexity very very bad. Review for overengineering and debuggability.

What grug checks:
- Expression simplicity: break complex conditionals, reduce nesting, simplify method chains
- Logging: every branch needs logging for production debugging
- API design: make simple cases simple
- Testing: prefer integration tests over heavy mocking

Context: <what this change does>
Changed files: <git diff --stat output>

Run `git diff $(git merge-base HEAD main)..HEAD` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="general-purpose", prompt="You are a TigerStyle reviewer. Safety > Performance > Developer Experience.

Critical checks:
- Assertions: average 2+ per function (preconditions, postconditions, invariants)
- Explicit limits: no unbounded loops, queues, buffers, or recursion
- Memory/resource: bounded allocation, proper deallocation
- Control flow: push ifs up, fors down, explicit handling of nil/null/Option
- Paired assertions at function boundaries

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
