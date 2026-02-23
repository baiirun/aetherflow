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
	PoolSize     int                 `json:"pool_size"`
	PoolMode     PoolMode            `json:"pool_mode"`
	Project      string              `json:"project"`
	SpawnPolicy  SpawnPolicy         `json:"spawn_policy"`
	Agents       []AgentStatus       `json:"agents"`
	Spawns       []SpawnStatus       `json:"spawns,omitempty"`
	RemoteSpawns []RemoteSpawnStatus `json:"remote_spawns,omitempty"`
	Queue        []Task              `json:"queue"`
	Errors       []string            `json:"errors,omitempty"`
}

// RemoteSpawnStatus is the wire-safe view of a remote spawn for status responses.
// It omits internal fields (RequestID, ProviderOperation) that are only relevant
// to the daemon's idempotency logic and should not leak over the API.
type RemoteSpawnStatus struct {
	SpawnID           string           `json:"spawn_id"`
	Provider          string           `json:"provider"`
	ProviderSandboxID string           `json:"provider_sandbox_id,omitempty"`
	ServerRef         string           `json:"server_ref,omitempty"`
	SessionID         string           `json:"session_id,omitempty"`
	State             RemoteSpawnState `json:"state"`
	LastError         string           `json:"last_error,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

// SpawnStatus is the status of a spawned agent registered with the daemon.
type SpawnStatus struct {
	SpawnID   string     `json:"spawn_id"`
	PID       int        `json:"pid"`
	SessionID string     `json:"session_id,omitempty"`
	State     SpawnState `json:"state"`
	Prompt    string     `json:"prompt"`
	SpawnTime time.Time  `json:"spawn_time"`
	ExitedAt  time.Time  `json:"exited_at,omitempty"`
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

// StatusSources groups the data sources consulted by BuildFullStatus and
// BuildAgentDetail. Using a struct avoids a long positional parameter list
// and makes call sites self-documenting.
type StatusSources struct {
	Pool         *Pool
	Spawns       *SpawnRegistry
	RemoteSpawns *RemoteSpawnStore
	Events       *EventBuffer // used by BuildAgentDetail only
}

// BuildFullStatus assembles the full swarm status by enriching pool data
// with task metadata from prog. Each prog show call runs in its own goroutine
// with a per-call timeout. Partial failures are captured in the Errors slice
// rather than failing the entire request.
func BuildFullStatus(ctx context.Context, src StatusSources, cfg Config, runner CommandRunner) FullStatus {
	policy := cfg.SpawnPolicy.Normalized()

	status := FullStatus{
		PoolSize:    cfg.PoolSize,
		Project:     cfg.Project,
		SpawnPolicy: policy,
	}

	if src.Pool != nil {
		status.PoolMode = src.Pool.Mode()

		agents := src.Pool.Status()
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
	if src.Spawns != nil {
		entries := src.Spawns.List()
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

	// Include remote (provider-backed) spawns from the persistent store.
	if src.RemoteSpawns != nil {
		recs, err := src.RemoteSpawns.List()
		if err != nil {
			status.Errors = append(status.Errors, fmt.Sprintf("remote spawn store: %v", err))
		} else if len(recs) > 0 {
			// Map to wire-safe display type, omitting internal fields.
			display := make([]RemoteSpawnStatus, len(recs))
			for i, r := range recs {
				display[i] = RemoteSpawnStatus{
					SpawnID:           r.SpawnID,
					Provider:          r.Provider,
					ProviderSandboxID: r.ProviderSandboxID,
					ServerRef:         r.ServerRef,
					SessionID:         r.SessionID,
					State:             r.State,
					LastError:         truncateLastError(r.LastError),
					CreatedAt:         r.CreatedAt,
					UpdatedAt:         r.UpdatedAt,
				}
			}
			// List() returns most-recently-updated first; re-sort to
			// oldest-first for consistency with local spawns display.
			sort.Slice(display, func(i, j int) bool {
				return display[i].CreatedAt.Before(display[j].CreatedAt)
			})
			status.RemoteSpawns = display
		}
	}

	return status
}

// AgentDetail is the response for the status.agent RPC method.
// It provides a detailed view of a single agent with tool call history.
// For remote spawns, the RemoteSpawn field is populated with full provider
// details so agents get discrete fields instead of parsing TaskTitle.
type AgentDetail struct {
	AgentStatus
	RemoteSpawn *RemoteSpawnStatus `json:"remote_spawn,omitempty"`
	ToolCalls   []ToolCall         `json:"tool_calls"`
	Errors      []string           `json:"errors,omitempty"`
}

// StatusAgentParams are the parameters for the status.agent RPC method.
type StatusAgentParams struct {
	AgentName string `json:"agent_name"`
	Limit     int    `json:"limit,omitempty"` // max tool calls to return; 0 = default (20)
}

const defaultToolCallLimit = 20

// BuildAgentDetail assembles detailed status for a single agent.
// It fetches task metadata from prog and reads tool calls from the event buffer.
// Session ID comes from the agent's state (populated by claimSession on
// session.created events from the plugin).
// The lookup order is: pool → spawn registry → remote spawn store.
func BuildAgentDetail(ctx context.Context, src StatusSources, cfg Config, runner CommandRunner, params StatusAgentParams) (*AgentDetail, error) {
	// Find the agent in the pool by name.
	var agent *Agent
	if src.Pool != nil {
		agents := src.Pool.Status()
		for i := range agents {
			if string(agents[i].ID) == params.AgentName {
				agent = &agents[i]
				break
			}
		}
	}

	// Check the spawn registry if not found in pool.
	if agent == nil && src.Spawns != nil {
		if entry := src.Spawns.Get(params.AgentName); entry != nil {
			return buildSpawnDetail(entry, src.Events, params)
		}
	}

	// Check the remote spawn store if not found in pool or spawn registry.
	// Hard error: unlike BuildFullStatus which degrades gracefully for the
	// overview, a detail lookup for a specific agent should fail if the store
	// is unreadable — the user asked for this specific agent and partial
	// results would be misleading.
	if agent == nil && src.RemoteSpawns != nil {
		rec, err := src.RemoteSpawns.GetBySpawnID(params.AgentName)
		if err != nil {
			return nil, fmt.Errorf("looking up remote spawn %q: %w", params.AgentName, err)
		}
		if rec != nil {
			return buildRemoteSpawnDetail(rec, src.Events, params)
		}
	}

	if agent == nil {
		return nil, fmt.Errorf("agent %q not found in pool, spawn registry, or remote spawn store", params.AgentName)
	}

	detail := &AgentDetail{
		AgentStatus: AgentStatus{
			ID:        string(agent.ID),
			TaskID:    agent.TaskID,
			Role:      string(agent.Role),
			PID:       agent.PID,
			SpawnTime: agent.SpawnTime,
			SessionID: agent.SessionID,
		},
	}

	detail.ToolCalls = extractToolCalls(src.Events, agent.SessionID, params.Limit)

	// Fetch task title + last log from prog (only when prog enrichment is relevant).
	if cfg.SpawnPolicy.Normalized().ProgEnrichmentEnabled() && agent.TaskID != "" {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		title, lastLog, err := fetchTaskSummary(callCtx, agent.TaskID, cfg.Project, runner)
		if err != nil {
			detail.Errors = append(detail.Errors, fmt.Sprintf("prog show %s: %v", agent.TaskID, err))
		} else {
			detail.TaskTitle = title
			detail.LastLog = lastLog
		}
	}

	return detail, nil
}

// maxTitleDisplayRunes is the maximum rune length for prompt display in status views.
const maxTitleDisplayRunes = 80

// buildSpawnDetail assembles a detail view for a spawned agent.
// Unlike pool agents, spawned agents don't have a prog task — the prompt is the spec.
// Session ID comes from the spawn entry (populated by claimSession).
func buildSpawnDetail(entry *SpawnEntry, events *EventBuffer, params StatusAgentParams) (*AgentDetail, error) {
	detail := &AgentDetail{
		AgentStatus: AgentStatus{
			ID:        entry.SpawnID,
			Role:      string(RoleSpawn),
			PID:       entry.PID,
			SpawnTime: entry.SpawnTime,
			SessionID: entry.SessionID,
			TaskTitle: truncatePrompt(entry.Prompt, maxTitleDisplayRunes),
		},
	}

	detail.ToolCalls = extractToolCalls(events, entry.SessionID, params.Limit)

	return detail, nil
}

// maxLastErrorRunes caps LastError in wire-type mapping to avoid leaking raw
// provider API response bodies into status output and the JSON store.
const maxLastErrorRunes = 256

// extractToolCalls returns tool calls from the event buffer for a given session.
// Centralizes the limit-defaulting and nil-checking that was duplicated across
// the three detail builders.
func extractToolCalls(events *EventBuffer, sessionID string, limit int) []ToolCall {
	if limit <= 0 {
		limit = defaultToolCallLimit
	}
	if events == nil || sessionID == "" {
		return nil
	}
	return ToolCallsFromEvents(events.Events(sessionID), limit)
}

// buildRemoteSpawnDetail assembles a detail view for a remote (provider-backed) spawn.
// Unlike pool agents or local spawns, remote spawns don't have a PID or prompt — the
// provider and state are the primary identifiers. Tool calls come from the event buffer
// if a session has been established.
func buildRemoteSpawnDetail(rec *RemoteSpawnRecord, events *EventBuffer, params StatusAgentParams) (*AgentDetail, error) {
	if rec == nil {
		return nil, fmt.Errorf("buildRemoteSpawnDetail: rec must not be nil")
	}

	wireStatus := RemoteSpawnStatus{
		SpawnID:           rec.SpawnID,
		Provider:          rec.Provider,
		ProviderSandboxID: rec.ProviderSandboxID,
		ServerRef:         rec.ServerRef,
		SessionID:         rec.SessionID,
		State:             rec.State,
		LastError:         truncateLastError(rec.LastError),
		CreatedAt:         rec.CreatedAt,
		UpdatedAt:         rec.UpdatedAt,
	}

	detail := &AgentDetail{
		AgentStatus: AgentStatus{
			ID:        rec.SpawnID,
			Role:      string(RoleRemote),
			SpawnTime: rec.CreatedAt,
			SessionID: rec.SessionID,
			TaskTitle: fmt.Sprintf("[%s] %s", rec.Provider, rec.State),
		},
		RemoteSpawn: &wireStatus,
	}

	if rec.LastError != "" {
		detail.Errors = append(detail.Errors, truncateLastError(rec.LastError))
	}

	detail.ToolCalls = extractToolCalls(events, rec.SessionID, params.Limit)

	return detail, nil
}

// truncateLastError sanitizes the error string at the wire-type boundary.
// Provider errors can contain full HTTP response bodies; we cap them here
// so they don't leak into af status or af status --json unsanitized.
// Uses rune-based truncation to avoid splitting multi-byte UTF-8 sequences.
func truncateLastError(s string) string {
	runes := []rune(s)
	if len(runes) <= maxLastErrorRunes {
		return s
	}
	return string(runes[:maxLastErrorRunes]) + "...[truncated]"
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
