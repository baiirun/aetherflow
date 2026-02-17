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
	PoolSize    int           `json:"pool_size"`
	PoolMode    PoolMode      `json:"pool_mode"`
	Project     string        `json:"project"`
	SpawnPolicy SpawnPolicy   `json:"spawn_policy"`
	Agents      []AgentStatus `json:"agents"`
	Spawns      []SpawnStatus `json:"spawns,omitempty"`
	Queue       []Task        `json:"queue"`
	Errors      []string      `json:"errors,omitempty"`
}

// SpawnStatus is the status of a spawned agent registered with the daemon.
type SpawnStatus struct {
	SpawnID   string    `json:"spawn_id"`
	PID       int       `json:"pid"`
	Prompt    string    `json:"prompt"`
	LogPath   string    `json:"log_path"`
	SpawnTime time.Time `json:"spawn_time"`
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
	SessionID string    `json:"session_id,omitempty"`
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
func BuildFullStatus(ctx context.Context, pool *Pool, spawns *SpawnRegistry, cfg Config, runner CommandRunner) FullStatus {
	policy := cfg.SpawnPolicy.Normalized()

	status := FullStatus{
		PoolSize:    cfg.PoolSize,
		Project:     cfg.Project,
		SpawnPolicy: policy,
	}

	if pool != nil {
		status.PoolMode = pool.Mode()

		agents := pool.Status()
		enriched := make([]AgentStatus, len(agents))
		for i, agent := range agents {
			enriched[i] = AgentStatus{
				ID:        string(agent.ID),
				TaskID:    agent.TaskID,
				Role:      string(agent.Role),
				PID:       agent.PID,
				SpawnTime: agent.SpawnTime,
			}
		}

		// In manual mode, status must be prog-optional. Return pool snapshots
		// only and skip all prog-dependent enrichment/queue calls.
		if policy.ProgEnrichmentEnabled() {
			var mu sync.Mutex
			var errors []string
			var wg sync.WaitGroup

			for i, agent := range agents {
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
			status.Errors = errors
			if queueErr != nil {
				status.Errors = append(status.Errors, fmt.Sprintf("prog ready: %v", queueErr))
			}
			status.Queue = queue
		}

		// Sort by spawn time, oldest first — stable ordering for humans.
		sort.Slice(enriched, func(i, j int) bool {
			return enriched[i].SpawnTime.Before(enriched[j].SpawnTime)
		})
		status.Agents = enriched
	}

	// Include spawned agents from the registry.
	if spawns != nil {
		entries := spawns.List()
		if len(entries) > 0 {
			spawned := make([]SpawnStatus, len(entries))
			for i, e := range entries {
				spawned[i] = SpawnStatus(e)
			}
			// Sort by spawn time, oldest first.
			sort.Slice(spawned, func(i, j int) bool {
				return spawned[i].SpawnTime.Before(spawned[j].SpawnTime)
			})
			status.Spawns = spawned
		}
	}

	return status
}

// AgentDetail is the response for the status.agent RPC method.
// It provides a detailed view of a single agent with tool call history.
type AgentDetail struct {
	AgentStatus
	ToolCalls []ToolCall `json:"tool_calls"`
	Errors    []string   `json:"errors,omitempty"`
}

// StatusAgentParams are the parameters for the status.agent RPC method.
type StatusAgentParams struct {
	AgentName string `json:"agent_name"`
	Limit     int    `json:"limit,omitempty"` // max tool calls to return; 0 = default (20)
}

const defaultToolCallLimit = 20

// BuildAgentDetail assembles detailed status for a single agent.
// It fetches task metadata from prog and parses tool calls from the JSONL log.
func BuildAgentDetail(ctx context.Context, pool *Pool, spawns *SpawnRegistry, cfg Config, runner CommandRunner, params StatusAgentParams) (*AgentDetail, error) {
	// Find the agent in the pool by name.
	var agent *Agent
	if pool != nil {
		agents := pool.Status()
		for i := range agents {
			if string(agents[i].ID) == params.AgentName {
				agent = &agents[i]
				break
			}
		}
	}

	// Check the spawn registry if not found in pool.
	if agent == nil && spawns != nil {
		if entry := spawns.Get(params.AgentName); entry != nil {
			return buildSpawnDetail(ctx, entry, cfg, params)
		}
	}

	if agent == nil {
		return nil, fmt.Errorf("agent %q not found in pool or spawn registry", params.AgentName)
	}

	detail := &AgentDetail{
		AgentStatus: AgentStatus{
			ID:        string(agent.ID),
			TaskID:    agent.TaskID,
			Role:      string(agent.Role),
			PID:       agent.PID,
			SpawnTime: agent.SpawnTime,
		},
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultToolCallLimit
	}

	// Fetch task metadata and tool calls concurrently.
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []string

	// Fetch task title + last log from prog.
	wg.Add(1)
	go func() {
		defer wg.Done()
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		title, lastLog, err := fetchTaskSummary(callCtx, agent.TaskID, cfg.Project, runner)
		if err != nil {
			mu.Lock()
			errors = append(errors, fmt.Sprintf("prog show %s: %v", agent.TaskID, err))
			mu.Unlock()
			return
		}
		detail.TaskTitle = title
		detail.LastLog = lastLog
	}()

	// Parse tool calls and session ID from JSONL log.
	wg.Add(1)
	go func() {
		defer wg.Done()
		parseCtx, parseCancel := context.WithTimeout(ctx, 5*time.Second)
		defer parseCancel()

		path := logFilePath(cfg.LogDir, agent.TaskID)
		calls, skipped, err := ParseToolCalls(parseCtx, path, limit)
		if err != nil {
			mu.Lock()
			errors = append(errors, fmt.Sprintf("parsing log %s: %v", path, err))
			mu.Unlock()
			return
		}
		if skipped > 0 {
			mu.Lock()
			errors = append(errors, fmt.Sprintf("skipped %d malformed lines in %s", skipped, path))
			mu.Unlock()
		}
		detail.ToolCalls = calls

		sessionID, err := ParseSessionID(parseCtx, path)
		if err != nil {
			mu.Lock()
			errors = append(errors, fmt.Sprintf("parsing session ID from %s: %v", path, err))
			mu.Unlock()
		} else {
			detail.SessionID = sessionID
		}
	}()

	wg.Wait()

	detail.Errors = errors

	return detail, nil
}

// maxTitleDisplayRunes is the maximum rune length for prompt display in status views.
const maxTitleDisplayRunes = 80

// buildSpawnDetail assembles a detail view for a spawned agent.
// Unlike pool agents, spawned agents don't have a prog task — the prompt is the spec.
func buildSpawnDetail(ctx context.Context, entry *SpawnEntry, cfg Config, params StatusAgentParams) (*AgentDetail, error) {
	detail := &AgentDetail{
		AgentStatus: AgentStatus{
			ID:        entry.SpawnID,
			TaskID:    "",
			Role:      string(RoleSpawn),
			PID:       entry.PID,
			SpawnTime: entry.SpawnTime,
			TaskTitle: truncatePrompt(entry.Prompt, maxTitleDisplayRunes),
		},
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultToolCallLimit
	}

	// Parse tool calls from JSONL log with a dedicated timeout.
	toolCtx, toolCancel := context.WithTimeout(ctx, 5*time.Second)
	defer toolCancel()

	calls, skipped, err := ParseToolCalls(toolCtx, entry.LogPath, limit)
	if err != nil {
		detail.Errors = append(detail.Errors, fmt.Sprintf("parsing log %s: %v", entry.LogPath, err))
	} else {
		if skipped > 0 {
			detail.Errors = append(detail.Errors, fmt.Sprintf("skipped %d malformed lines in %s", skipped, entry.LogPath))
		}
		detail.ToolCalls = calls
	}

	// Parse session ID with its own timeout — don't share the tool call timeout.
	sessCtx, sessCancel := context.WithTimeout(ctx, 2*time.Second)
	defer sessCancel()

	sessionID, err := ParseSessionID(sessCtx, entry.LogPath)
	if err != nil {
		detail.Errors = append(detail.Errors, fmt.Sprintf("parsing session ID from %s: %v", entry.LogPath, err))
	} else {
		detail.SessionID = sessionID
	}

	return detail, nil
}

// truncatePrompt shortens a user prompt for display in status views.
func truncatePrompt(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "\u2026"
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
