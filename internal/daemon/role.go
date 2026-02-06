package daemon

import (
	"context"
	"encoding/json"
	"fmt"
)

// Role is the agent role assigned to a task.
type Role string

const (
	RolePlanner Role = "planner"
	RoleWorker  Role = "worker"
)

// TaskMeta holds task metadata from `prog show --json`.
// Only the fields needed for role inference are included.
type TaskMeta struct {
	ID               string   `json:"id"`
	Type             string   `json:"type"`
	DefinitionOfDone string   `json:"definition_of_done"`
	Labels           []string `json:"labels"`
}

// InferRole determines the agent role for a task.
//
// For MVP, all tasks are assigned the worker role. The planner role
// will be added when we have a clear heuristic for distinguishing
// planning tasks from execution tasks (DoD presence alone isn't enough
// since planning tasks can also have DoDs).
func InferRole(_ TaskMeta) Role {
	return RoleWorker
}

// FetchTaskMeta retrieves task metadata from prog via `prog show --json`.
func FetchTaskMeta(ctx context.Context, taskID string, project string, runner CommandRunner) (TaskMeta, error) {
	if runner == nil {
		runner = ExecCommandRunner
	}

	args := []string{"show", taskID, "--json"}
	if project != "" {
		args = append(args, "-p", project)
	}

	output, err := runner(ctx, "prog", args...)
	if err != nil {
		return TaskMeta{}, fmt.Errorf("prog show %s: %w (output: %s)", taskID, err, string(output))
	}

	var meta TaskMeta
	if err := json.Unmarshal(output, &meta); err != nil {
		return TaskMeta{}, fmt.Errorf("parsing prog show output for %s: %w", taskID, err)
	}

	return meta, nil
}
