---
module: daemon
date: 2026-02-07
problem_type: logic_error
component: tooling
symptoms:
  - "reconciler never marks PR-merged tasks as done because local main ref is stale"
  - "task IDs from prog list flow unsanitized into git commands, enabling flag injection"
  - "isBranchMerged error classification relies on fragile string matching across git versions"
  - "solo mode merge instructions miss git pull and conflict abort handling"
  - "reconciler goroutine starts unnecessarily in solo mode"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - reconciler
  - git-fetch
  - injection
  - input-validation
  - error-handling
  - solo-mode
  - code-review
  - multi-agent-review
  - go
  - trust-boundary
---

# Reconciler Correctness Fixes and Solo Mode Hardening

## Problem

After implementing the merge reconciler (`reconcileOnce`) and solo mode (`--solo` flag), a 7-agent parallel code review surfaced 9 findings ranging from a complete functional failure (reconciler permanently no-op in normal mode) to security issues (task ID injection into git commands) and operational gaps (missing conflict handling in solo landing instructions). All issues passed initial tests because they only manifest under real-world conditions: remote repositories, adversarial input, varying git versions, or conflict scenarios.

## Environment

- Module: `internal/daemon` (reconcile.go, config.go, poll.go, reclaim.go, prompt.go, daemon.go)
- Go version: 1.25.5
- Affected components: `reconcileOnce`, `isBranchMerged`, `fetchReviewingTasks`, `fetchInProgressTasks`, `landStepsSolo`, `Config.Validate`
- Date: 2026-02-07

## Symptoms

- In normal (PR-based) mode, tasks stuck in "reviewing" status indefinitely because the reconciler checked a stale local `main` ref that was never updated
- A crafted task ID like `--exec=evil` from `prog list --json` would be passed directly to `git rev-parse --verify --exec=evil` and interpreted as a flag
- `isBranchMerged` used `strings.Contains(errStr, "not a valid")` to distinguish git errors, which breaks across git versions, locales, and error message changes
- Solo mode agents could merge into a stale local `main` (no `git pull` before merge) and had no instructions for aborting unresolvable conflicts
- Reconciler goroutine ran even in solo mode where it serves no purpose (solo agents call `prog done` directly)
- `ReconcileInterval` of 0 or negative values would cause `time.NewTicker` to panic
- Test mock for `prog done` checked `len(args) >= 1` but accessed `args[1]`, causing index-out-of-range on malformed commands
- `fetchReviewingTasks` and `fetchInProgressTasks` were near-duplicate functions parsing `prog list --json` with identical logic

## What Didn't Work

**Direct solution:** All 9 findings were identified through parallel multi-agent code review and addressed in a single implementation pass. The findings were invisible to unit tests because they involve real-world conditions (remote git repos, adversarial input, git version differences) that mocks don't replicate.

## Solution

### P1-1: Reconciler fetches remote before merge-base check

```go
// Before: reconcileOnce() jumped straight to checking reviewing tasks
func (d *Daemon) reconcileOnce(ctx context.Context) {
    tasks, err := fetchReviewingTasks(ctx, d.config.Project, d.config.Runner)
    // ... merge-base checks against STALE local main
}

// After: fetch origin main first, gracefully handle no-remote case
func (d *Daemon) reconcileOnce(ctx context.Context) {
    _, err := d.config.Runner(ctx, "git", "fetch", "origin", "main")
    if err != nil {
        // No remote configured (local-only repos) -- continue with local state
        d.log.Debug("reconcile: git fetch origin main failed (no remote?)", "error", err)
    }

    tasks, err := fetchReviewingTasks(ctx, d.config.Project, d.config.Runner, d.log)
    // ... merge-base checks now reflect actual remote state
}
```

### P1-2: Task ID validation at trust boundary

```go
// config.go -- regex alongside existing validProjectName
var validTaskID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// poll.go -- shared fetchTasksByStatus validates IDs before returning
func fetchTasksByStatus(ctx context.Context, project, status string,
    runner CommandRunner, log *slog.Logger) ([]progListItem, error) {
    // ... parse JSON ...
    valid := items[:0]
    for _, item := range items {
        if !validTaskID.MatchString(item.ID) {
            log.Warn("fetchTasksByStatus: skipping task with invalid ID",
                "id", item.ID, "status", status)
            continue
        }
        valid = append(valid, item)
    }
    return valid, nil
}
```

### P1-3: Simplified error handling in isBranchMerged

```go
// Before: fragile string matching on git error messages
errStr := strings.ToLower(err.Error())
if strings.Contains(errStr, "not a valid") || strings.Contains(errStr, "unknown revision") {
    return false, fmt.Errorf("git merge-base failed: %w", err)
}

// After: since rev-parse already confirmed branch exists, any merge-base
// error means "not ancestor" (exit code 1). No string matching needed.
_, err = runner(ctx, "git", "merge-base", "--is-ancestor", branch, "main")
if err != nil {
    return mergeResult{merged: false}, nil
}
```

### P2-4: Branch-missing detection with logging

```go
// Before: isBranchMerged returned (bool, error) -- missing branch was invisible
return true, nil // branch doesn't exist, but caller has no idea why

// After: returns mergeResult struct with branchMissing flag
type mergeResult struct {
    merged        bool
    branchMissing bool
}

// Caller logs the important decision:
if result.branchMissing {
    d.log.Warn("reconcile: branch missing, treating as merged",
        "task", task.ID, "branch", "af/"+task.ID)
}
```

### P2-5: Skip reconciler in solo mode

```go
// Before: reconciler started unconditionally
go d.reconcileReviewing(ctx)

// After: solo agents call prog done directly, nothing to reconcile
if !d.config.Solo {
    go d.reconcileReviewing(ctx)
}
```

### P2-6: Solo landing instructions hardened

Added `git pull origin main` before merge, and abort + `prog block` instructions for unresolvable conflicts:

```
3. Pull latest main -- git checkout main && git pull origin main
4. Merge to main -- git merge af/{{task_id}} --no-ff
   If conflicts too complex: git merge --abort && prog block {{task_id}} "..."
```

### P2-7: ReconcileInterval validation

```go
// In Config.Validate():
if c.ReconcileInterval < 5*time.Second {
    return fmt.Errorf("reconcile-interval must be at least 5s, got %v", c.ReconcileInterval)
}
```

### P2-8: Test mock bounds check

```go
// Before:
if name == "prog" && len(args) >= 1 && args[0] == "done" {
    r.doneCalls = append(r.doneCalls, args[1]) // potential panic

// After:
if name == "prog" && len(args) >= 2 && args[0] == "done" {
```

### P3-9: Extracted shared fetchTasksByStatus

`fetchReviewingTasks` and `fetchInProgressTasks` both parsed `prog list --json` with identical validation logic. Extracted a shared `fetchTasksByStatus(ctx, project, status, runner, log)` function in `poll.go`. Moved `progListItem` struct there too. Both callers now delegate to the shared function.

## Why This Works

**Stale main ref (P1-1):** The reconciler's job is to detect when branches land on `main`. In PR-based workflows, merges happen on GitHub, so local `main` never advances. Without `git fetch origin main`, the `merge-base --is-ancestor` check always returns false, making the entire reconciler a permanent no-op. The fix fetches before checking, and gracefully degrades when no remote exists (expected for local-only solo setups).

**Task ID injection (P1-2):** `prog list --json` is an external process returning JSON. A compromised or buggy prog could return an ID like `--exec=evil` which git would interpret as a flag rather than a ref name. Validating at the boundary (immediately after parsing JSON) prevents injection into all downstream consumers (`git rev-parse`, `git merge-base`, `prog done`).

**Error string matching (P1-3):** Git error messages vary across versions and locales. The original code tried to distinguish "not ancestor" (expected) from "actual error" by matching error text. But since `rev-parse --verify` already confirmed the branch exists, there's only one error case left: "not an ancestor." The simplified logic is both more correct and more maintainable.

**Branch-missing visibility (P2-4):** Treating a missing branch as "merged" is the correct semantic (branch was cleaned up after merge), but it's a significant operational decision that was invisible. The `mergeResult` struct makes this explicit without adding complexity to the interface.

**Solo reconciler guard (P2-5):** Solo agents merge directly and call `prog done` themselves. They never use `prog review`, so there are never "reviewing" tasks to reconcile. The goroutine was harmless but wasteful.

**Solo merge instructions (P2-6):** Without `git pull origin main`, a solo agent could merge into a stale local main, creating divergence. Without abort instructions, an agent facing complex conflicts would be stuck with no recovery path.

## Prevention

- **Stale local refs:** Any daemon feature that checks git state should consider whether it's checking local or remote state. If the answer involves remote activity (PR merges, CI, other developers), a `git fetch` is mandatory before the check
- **External input validation:** Any data from external processes (`prog`, `git`, APIs) crossing a trust boundary into command arguments must be validated. The regex-at-boundary pattern (`validTaskID.MatchString`) is cheap and catches flag injection, path traversal, and shell metacharacters
- **Avoid string matching on tool output:** Never match on error message text from external tools. Use exit codes, structured output, or eliminate the need to distinguish error types through prior validation (like the rev-parse gate)
- **Return structs over booleans for multi-signal results:** When a function makes decisions with different implications (merged-via-ancestor vs merged-via-cleanup), a struct communicates the distinction better than multiple return values
- **Guard idle goroutines:** If a background goroutine has no work to do in a particular mode, don't start it. Log the skip decision for operational clarity
- **Test mock fidelity:** Mock command handlers should validate argument counts match what they access. Use `len(args) >= N` where N covers the highest index you read
- **Deduplicate fetch functions early:** When two functions parse the same external output with the same validation, extract immediately. Duplicates drift: one gets the validation fix, the other doesn't

## Related Issues

- See also: [post-review-hardening-findings-daemon](../best-practices/post-review-hardening-findings-daemon-20260207.md) -- previous round of multi-agent review findings (dead code, terminal injection, context cancellation)
- See also: [daemon-cross-project-shutdown-socket-isolation](../security-issues/daemon-cross-project-shutdown-socket-isolation-20260207.md) -- socket path isolation and input validation patterns
