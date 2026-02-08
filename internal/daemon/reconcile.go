package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// fetchReviewingTasks queries prog for tasks in "reviewing" status for this project.
func fetchReviewingTasks(ctx context.Context, project string, runner CommandRunner) ([]progListItem, error) {
	output, err := runner(ctx, "prog", "list", "--status", "reviewing", "--type", "task", "--json", "-p", project)
	if err != nil {
		return nil, fmt.Errorf("prog list --status reviewing: %w (output: %s)", err, string(output))
	}

	var items []progListItem
	if err := json.Unmarshal(output, &items); err != nil {
		return nil, fmt.Errorf("parsing prog list output: %w", err)
	}

	return items, nil
}

// isBranchMerged checks whether the branch af/<taskID> has been merged into main.
// Returns true if:
//   - The branch is an ancestor of main (git merge-base --is-ancestor succeeds)
//   - The branch doesn't exist (already cleaned up — treat as merged)
//
// Returns false if the branch exists but hasn't been merged yet.
func isBranchMerged(ctx context.Context, taskID string, runner CommandRunner) (bool, error) {
	branch := "af/" + taskID

	// Check if the branch exists first.
	_, err := runner(ctx, "git", "rev-parse", "--verify", branch)
	if err != nil {
		// Branch doesn't exist. If it was cleaned up after merge, that's
		// the expected state. Treat as merged.
		return true, nil
	}

	// Branch exists — check if it's been merged into main.
	_, err = runner(ctx, "git", "merge-base", "--is-ancestor", branch, "main")
	if err != nil {
		// Non-zero exit means the branch is NOT an ancestor of main.
		// Check if it's actually a git error vs just "not ancestor".
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "not a valid") || strings.Contains(errStr, "unknown revision") {
			return false, fmt.Errorf("git merge-base failed: %w", err)
		}
		// Exit code 1 = not an ancestor = not merged.
		return false, nil
	}

	return true, nil
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
	tasks, err := fetchReviewingTasks(ctx, d.config.Project, d.config.Runner)
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

		merged, err := isBranchMerged(ctx, task.ID, d.config.Runner)
		if err != nil {
			d.log.Warn("reconcile: failed to check branch status",
				"task", task.ID,
				"error", err,
			)
			continue
		}

		if !merged {
			d.log.Debug("reconcile: branch not yet merged",
				"task", task.ID,
				"branch", "af/"+task.ID,
			)
			continue
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
