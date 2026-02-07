package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// FullStatus is the response for the status.full RPC method.
// It enriches the live pool data with task metadata from prog.
type FullStatus struct {
	PoolSize int           `json:"pool_size"`
	Project  string        `json:"project"`
	Agents   []AgentStatus `json:"agents"`
	Queue    []Task        `json:"queue"`
	Errors   []string      `json:"errors,omitempty"`
}

// AgentStatus enriches an Agent with task metadata from prog.
type AgentStatus struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	Role      string    `json:"role"`
	PID       int       `json:"pid"`
	SpawnTime time.Time `json:"spawn_time"`
	TaskTitle string    `json:"task_title"`
	LastLog   string    `json:"last_log,omitempty"`
}

// taskShowResponse is the sparse parse target for `prog show --json`.
// Only the fields needed for status display are included.
type taskShowResponse struct {
	Title string `json:"title"`
	Logs  []struct {
		Message   string `json:"message"`
		CreatedAt string `json:"created_at"`
	} `json:"logs"`
}

// BuildFullStatus assembles the full swarm status by enriching pool data
// with task metadata from prog. Each prog show call runs in its own goroutine
// with a per-call timeout. Partial failures are captured in the Errors slice
// rather than failing the entire request.
func BuildFullStatus(ctx context.Context, pool *Pool, cfg Config, runner CommandRunner) FullStatus {
	status := FullStatus{
		PoolSize: cfg.PoolSize,
		Project:  cfg.Project,
	}

	if pool == nil {
		return status
	}

	agents := pool.Status()

	// Enrich each agent with task metadata from prog, and fetch the
	// pending queue, all in parallel. Partial failures are collected
	// in the errors slice rather than failing the entire request.
	enriched := make([]AgentStatus, len(agents))
	var mu sync.Mutex
	var errors []string
	var wg sync.WaitGroup

	for i, agent := range agents {
		enriched[i] = AgentStatus{
			ID:        string(agent.ID),
			TaskID:    agent.TaskID,
			Role:      string(agent.Role),
			PID:       agent.PID,
			SpawnTime: agent.SpawnTime,
		}

		wg.Add(1)
		go func(idx int, taskID string) {
			defer wg.Done()

			callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			title, lastLog, err := fetchTaskSummary(callCtx, taskID, cfg.Project, runner)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Sprintf("prog show %s: %v", taskID, err))
				mu.Unlock()
				return
			}

			// No lock needed — each goroutine writes to its own index.
			enriched[idx].TaskTitle = title
			enriched[idx].LastLog = lastLog
		}(i, agent.TaskID)
	}

	// Fetch the pending queue concurrently with agent enrichment.
	var queue []Task
	var queueErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		queueCtx, queueCancel := context.WithTimeout(ctx, 5*time.Second)
		defer queueCancel()
		queue, queueErr = fetchQueue(queueCtx, cfg.Project, runner)
	}()

	wg.Wait()

	// Sort by spawn time, oldest first — stable ordering for humans.
	sort.Slice(enriched, func(i, j int) bool {
		return enriched[i].SpawnTime.Before(enriched[j].SpawnTime)
	})

	status.Agents = enriched
	status.Errors = errors

	if queueErr != nil {
		status.Errors = append(status.Errors, fmt.Sprintf("prog ready: %v", queueErr))
	}
	status.Queue = queue

	return status
}

// fetchTaskSummary calls prog show --json and extracts the title and last log message.
func fetchTaskSummary(ctx context.Context, taskID, project string, runner CommandRunner) (title, lastLog string, err error) {
	args := []string{"show", taskID, "--json"}
	if project != "" {
		args = append(args, "-p", project)
	}

	output, err := runner(ctx, "prog", args...)
	if err != nil {
		return "", "", fmt.Errorf("%w (output: %s)", err, string(output))
	}

	var resp taskShowResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return "", "", fmt.Errorf("parsing output: %w", err)
	}

	if len(resp.Logs) > 0 {
		lastLog = resp.Logs[len(resp.Logs)-1].Message
	}

	return resp.Title, lastLog, nil
}

// fetchQueue calls prog ready and returns the pending tasks.
func fetchQueue(ctx context.Context, project string, runner CommandRunner) ([]Task, error) {
	output, err := runner(ctx, "prog", "ready", "-p", project)
	if err != nil {
		return nil, fmt.Errorf("%w (output: %s)", err, string(output))
	}

	tasks, err := ParseProgReady(string(output))
	if err != nil {
		return nil, fmt.Errorf("parsing output: %w", err)
	}

	return tasks, nil
}
