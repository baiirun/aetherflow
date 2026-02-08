package daemon

import (
	"context"
	"log/slog"
)

// fetchInProgressTasks queries prog for tasks currently in_progress for this project.
// Returns only tasks (not epics) since epics don't have agents assigned to them.
func fetchInProgressTasks(ctx context.Context, project string, runner CommandRunner, log *slog.Logger) ([]Task, error) {
	items, err := fetchTasksByStatus(ctx, project, "in_progress", runner, log)
	if err != nil {
		return nil, err
	}

	tasks := make([]Task, 0, len(items))
	for _, item := range items {
		tasks = append(tasks, Task{
			ID:    item.ID,
			Title: item.Title,
		})
	}

	return tasks, nil
}

// Reclaim spawns agents for in_progress tasks that have no running agent.
// This handles the case where the daemon crashed or was stopped while agents
// were running — the tasks stay in_progress in prog but have no process.
//
// Call after SetContext so p.ctx is available for respawn goroutines.
func (p *Pool) Reclaim(ctx context.Context) {
	p.log.Info("reclaim: checking for orphaned in_progress tasks",
		"project", p.config.Project,
	)

	// Don't reclaim if pool is paused — operator intentionally stopped work.
	p.mu.RLock()
	mode := p.mode
	p.mu.RUnlock()
	if mode == PoolPaused {
		p.log.Info("reclaim: skipped, pool is paused")
		return
	}

	tasks, err := fetchInProgressTasks(ctx, p.config.Project, p.runner, p.log)
	if err != nil {
		p.log.Error("reclaim: failed to fetch in_progress tasks", "error", err)
		return
	}

	if len(tasks) == 0 {
		p.log.Debug("reclaim: no orphaned tasks")
		return
	}

	reclaimed := 0
	skipped := 0
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}

		p.mu.RLock()
		_, alreadyRunning := p.agents[task.ID]
		count := p.runningCount()
		p.mu.RUnlock()

		if alreadyRunning {
			skipped++
			continue
		}

		if count >= p.config.PoolSize {
			p.log.Info("reclaim: pool full, deferring remaining orphans",
				"reclaimed", reclaimed,
				"deferred", len(tasks)-reclaimed-skipped,
			)
			break
		}

		// Infer role from task metadata, same as spawn().
		meta, err := FetchTaskMeta(ctx, task.ID, p.config.Project, p.runner)
		if err != nil {
			p.log.Error("reclaim: failed to fetch task metadata",
				"task_id", task.ID,
				"error", err,
			)
			continue
		}
		role := InferRole(meta)

		// Use respawn path — task is already in_progress, no need for prog start.
		p.log.Info("reclaim: respawning orphaned task",
			"task_id", task.ID,
			"role", role,
		)
		p.respawn(task.ID, role)
		reclaimed++
	}

	if reclaimed > 0 {
		p.log.Info("reclaim complete", "reclaimed", reclaimed, "total_orphans", len(tasks))
	}
}
