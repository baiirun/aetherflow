package daemon

import (
	"context"
	"log/slog"
	"time"
)

// fetchReviewingTasks queries prog for tasks in "reviewing" status for this project.
func fetchReviewingTasks(ctx context.Context, project string, runner CommandRunner, log *slog.Logger) ([]progListItem, error) {
	return fetchTasksByStatus(ctx, project, "reviewing", runner, log)
}

// mergeResult describes how isBranchMerged determined its result.
type mergeResult struct {
	merged        bool
	branchMissing bool // true when the branch doesn't exist (treated as merged)
}

// isBranchMerged checks whether the branch af/<taskID> has been merged into main.
// Returns merged=true if:
//   - The branch is an ancestor of main (git merge-base --is-ancestor succeeds)
//   - The branch doesn't exist (already cleaned up — treat as merged, branchMissing=true)
//
// Returns merged=false if the branch exists but hasn't been merged yet.
func isBranchMerged(ctx context.Context, taskID string, runner CommandRunner) (mergeResult, error) {
	branch := "af/" + taskID

	// Check if the branch exists first.
	_, err := runner(ctx, "git", "rev-parse", "--verify", branch)
	if err != nil {
		// Branch doesn't exist. If it was cleaned up after merge, that's
		// the expected state. Treat as merged.
		return mergeResult{merged: true, branchMissing: true}, nil
	}

	// Branch exists — check if it's been merged into main.
	// Since rev-parse already confirmed the branch exists, any error from
	// merge-base means "not an ancestor" (exit code 1). No need to
	// distinguish error types via fragile string matching.
	_, err = runner(ctx, "git", "merge-base", "--is-ancestor", branch, "main")
	if err != nil {
		return mergeResult{merged: false}, nil
	}

	return mergeResult{merged: true}, nil
}

// reconcileReviewing runs on a timer, checking if reviewing tasks have been
// merged to main and marking them done. This closes the loop between an agent
// calling `prog review` (work complete, awaiting merge) and the task reaching
// its terminal `done` state after the branch lands on main.
func (d *Daemon) reconcileReviewing(ctx context.Context) {
	d.log.Info("reconciler started",
		"project", d.config.Project,
		"interval", d.config.ReconcileInterval,
	)

	ticker := time.NewTicker(d.config.ReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.log.Info("reconciler stopped")
			return
		case <-ticker.C:
			d.reconcileOnce(ctx)
		}
	}
}

// reconcileOnce runs a single reconciliation pass.
func (d *Daemon) reconcileOnce(ctx context.Context) {
	// Update local main ref from remote so merge-base checks reflect actual
	// state. Without this, local main is stale and the reconciler becomes a
	// permanent no-op for PR-based workflows where merges happen on GitHub.
	_, err := d.config.Runner(ctx, "git", "fetch", "origin", "main")
	if err != nil {
		// No remote configured (e.g. local-only repos) — continue with
		// local state. This is expected for solo-mode setups.
		d.log.Debug("reconcile: git fetch origin main failed (no remote?)", "error", err)
	}

	tasks, err := fetchReviewingTasks(ctx, d.config.Project, d.config.Runner, d.log)
	if err != nil {
		// Context cancellation is expected during shutdown.
		if ctx.Err() != nil {
			return
		}
		d.log.Error("reconcile: failed to fetch reviewing tasks", "error", err)
		return
	}

	if len(tasks) == 0 {
		d.log.Debug("reconcile: no reviewing tasks")
		return
	}

	completed := 0
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}

		result, err := isBranchMerged(ctx, task.ID, d.config.Runner)
		if err != nil {
			d.log.Warn("reconcile: failed to check branch status",
				"task", task.ID,
				"error", err,
			)
			continue
		}

		if !result.merged {
			d.log.Debug("reconcile: branch not yet merged",
				"task", task.ID,
				"branch", "af/"+task.ID,
			)
			continue
		}

		if result.branchMissing {
			d.log.Warn("reconcile: branch missing, treating as merged",
				"task", task.ID,
				"branch", "af/"+task.ID,
			)
		}

		// Branch is merged (or cleaned up) — mark the task done.
		_, err = d.config.Runner(ctx, "prog", "done", task.ID)
		if err != nil {
			d.log.Error("reconcile: failed to mark task done",
				"task", task.ID,
				"error", err,
			)
			continue
		}

		d.log.Info("reconcile: task merged and marked done",
			"task", task.ID,
			"title", task.Title,
		)
		completed++
	}

	if completed > 0 {
		d.log.Info("reconcile complete",
			"completed", completed,
			"total_reviewing", len(tasks),
		)
	}
}
