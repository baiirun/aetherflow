---
module: tooling
date: 2026-02-07
problem_type: developer_experience
component: prog
symptoms:
  - "prog ready shows only the LAST task in a chain as ready instead of the first"
  - "prog show on the first task shows it depending on the second task"
  - "prog graph shows dependency chain is inverted from intended direction"
  - "tasks created with --blocks have reversed dependency semantics from expectation"
root_cause: misunderstood_semantics
resolution_type: workflow_fix
severity: medium
tags:
  - prog
  - dependencies
  - task-management
  - cli-semantics
  - workflow
  - blocks-flag
---

# prog add --blocks Creates Inverted Dependency Direction

## Problem

When creating a chain of sequential tasks using `prog add "Task N" --blocks <previous_task_id>`, the resulting dependency graph was inverted. The last task in the chain appeared as "ready" instead of the first, and the first task had a dependency on the second. The intent was to create a chain where each task depends on the previous one (task 2 waits for task 1, task 3 waits for task 2, etc.), but the `--blocks` flag has the opposite semantics from what was assumed.

## Environment

- Tool: `prog` task manager
- Context: Creating a 10-task TUI implementation plan with sequential dependencies
- Date: 2026-02-07

## Symptoms

- Created 10 TUI tasks with `prog add "Task N" --blocks <previous_task_id>`
- `prog ready -p aetherflow` showed only the LAST task (ts-f41595 "Visual polish") as ready, not the first
- `prog show ts-93e406` (first task) showed `Dependencies: ts-c9fa41` (second task)
- `prog graph -p aetherflow` showed the entire chain was inverted

## What Didn't Work

**Initial approach:** `prog add "Task 2" --blocks ts-task1` with the assumption that `--blocks` means "this new task is blocked by the specified ID." This interpretation felt natural ("I'm adding a task that is blocked by the prerequisite"), but it's wrong.

## Solution

1. Deleted all 10 tasks with `prog delete`
2. Recreated all 10 tasks without `--blocks`
3. Used `prog blocks <earlier_task> <later_task>` to set correct dependency direction
4. Verified with `prog ready` that only the first task appeared as ready
5. Verified with `prog show` that the second task depended on the first

### Correct semantics

```
# --blocks on prog add: "the NEW task blocks the SPECIFIED task"
prog add "Task X" --blocks ts-Y
# Result: X blocks Y → Y depends on X → Y cannot start until X is done

# prog blocks command: "A blocks B"
prog blocks A B
# Result: A blocks B → B depends on A → B cannot start until A is done
```

### Correct workflow for sequential task chains

```bash
# Step 1: Create all tasks without --blocks
prog add "Step 1: Foundation" -p myproject
prog add "Step 2: Core logic" -p myproject
prog add "Step 3: Polish" -p myproject

# Step 2: Wire dependencies with prog blocks <earlier> <later>
prog blocks ts-step1 ts-step2
prog blocks ts-step2 ts-step3

# Step 3: Verify
prog ready -p myproject   # Should show only Step 1
prog show ts-step2        # Should show Dependencies: ts-step1
```

## Why This Works

The `--blocks` flag on `prog add` means "this new task I am creating will block the task ID I specify." The subject is the new task, and the object is the existing task. So `prog add "X" --blocks Y` creates X and makes Y depend on X.

This is consistent with `prog blocks A B` where A is the blocker and B is the blocked. Both commands read left-to-right as "A blocks B."

The confusion arises because when creating a chain of tasks in sequence, the natural mental model is "this new task depends on (is blocked by) the previous one" — but `--blocks` expresses the inverse relationship.

## Prevention

- **Create tasks first, wire dependencies second.** When building task chains, create all tasks without `--blocks`, then use `prog blocks <earlier> <later>` to set dependencies. This two-step approach is less error-prone because `prog blocks` reads naturally as "earlier blocks later."
- **Always verify with `prog ready` after setting dependencies.** Only the first task in a chain should appear as ready. If the last task appears instead, the chain is inverted.
- **Read `prog blocks --help` for authoritative semantics.** The help text clarifies that `prog blocks A B` means "A blocks B" (B depends on A).
- **Mnemonic:** `--blocks` means "I block them", not "they block me." The new task is the subject, the specified ID is the object.
