package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Task represents a ready task from prog.
type Task struct {
	ID       string `json:"id"`
	Priority int    `json:"priority"`
	Title    string `json:"title"`
}

// progListItem is the sparse parse target for `prog list --json`.
// Only the fields needed for status-based queries are included.
type progListItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// CommandRunner executes a command and returns its combined output.
// This is the seam for testing — swap the real exec with a fake.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecCommandRunner runs a real command via os/exec.
func ExecCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// fetchTasksByStatus queries prog for tasks with a given status. Task IDs are
// validated before returning to prevent injection into git/shell commands.
func fetchTasksByStatus(ctx context.Context, project, status string, runner CommandRunner, log *slog.Logger) ([]progListItem, error) {
	output, err := runner(ctx, "prog", "list", "--status", status, "--type", "task", "--json", "-p", project)
	if err != nil {
		return nil, fmt.Errorf("prog list --status %s: %w (output: %s)", status, err, string(output))
	}

	var items []progListItem
	if err := json.Unmarshal(output, &items); err != nil {
		return nil, fmt.Errorf("parsing prog list output: %w", err)
	}

	valid := items[:0]
	for _, item := range items {
		if !validTaskID.MatchString(item.ID) {
			log.Warn("fetchTasksByStatus: skipping task with invalid ID",
				"id", item.ID,
				"status", status,
				"title", item.Title,
			)
			continue
		}
		valid = append(valid, item)
	}

	return valid, nil
}

// ParseProgReady parses the table output of `prog ready` into tasks.
//
// Expected format (space-padded columns, first line is header):
//
//	ID           PRI  TITLE
//	ts-1450cd    1    Daemon: poll loop
//	ep-82985b    2    Some epic title
func ParseProgReady(output string) ([]Task, error) {
	var tasks []Task

	scanner := bufio.NewScanner(strings.NewReader(output))

	// Skip header line
	if !scanner.Scan() {
		return nil, nil // Empty output, no tasks
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		task, err := parseTaskLine(line)
		if err != nil {
			return nil, fmt.Errorf("parsing line %q: %w", line, err)
		}
		tasks = append(tasks, task)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning output: %w", err)
	}

	return tasks, nil
}

// parseTaskLine parses a single line of prog ready output.
// The format is space-separated fields: ID, PRI, TITLE (where title is the rest).
func parseTaskLine(line string) (Task, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return Task{}, fmt.Errorf("expected at least 3 fields, got %d", len(fields))
	}

	pri, err := strconv.Atoi(fields[1])
	if err != nil {
		return Task{}, fmt.Errorf("parsing priority %q: %w", fields[1], err)
	}

	title := strings.Join(fields[2:], " ")

	return Task{
		ID:       fields[0],
		Priority: pri,
		Title:    title,
	}, nil
}

// Poller watches prog for ready tasks and sends them to a channel.
type Poller struct {
	project  string
	interval time.Duration
	run      CommandRunner
	log      *slog.Logger
}

// NewPoller creates a poller that checks prog for ready tasks.
func NewPoller(project string, interval time.Duration, runner CommandRunner, log *slog.Logger) *Poller {
	if runner == nil {
		runner = ExecCommandRunner
	}
	return &Poller{
		project:  project,
		interval: interval,
		run:      runner,
		log:      log,
	}
}

// Poll fetches ready tasks from prog once.
func (p *Poller) Poll(ctx context.Context) ([]Task, error) {
	output, err := p.run(ctx, "prog", "ready", "-p", p.project)
	if err != nil {
		return nil, fmt.Errorf("prog ready: %w (output: %s)", err, string(output))
	}

	tasks, err := ParseProgReady(string(output))
	if err != nil {
		return nil, fmt.Errorf("parsing prog ready output: %w", err)
	}

	return tasks, nil
}

// Start runs the poll loop, sending batches of ready tasks to the returned channel.
// The channel is closed when the context is cancelled.
func (p *Poller) Start(ctx context.Context) <-chan []Task {
	ch := make(chan []Task)

	go func() {
		defer close(ch)

		p.log.Info("poll loop started",
			"project", p.project,
			"interval", p.interval,
		)

		// Poll immediately on start, then on interval.
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		p.pollAndSend(ctx, ch)

		for {
			select {
			case <-ctx.Done():
				p.log.Info("poll loop stopped")
				return
			case <-ticker.C:
				p.pollAndSend(ctx, ch)
			}
		}
	}()

	return ch
}

func (p *Poller) pollAndSend(ctx context.Context, ch chan<- []Task) {
	tasks, err := p.Poll(ctx)
	if err != nil {
		// Context cancellation is expected during shutdown, don't log as error.
		if ctx.Err() != nil {
			return
		}
		p.log.Error("poll failed", "error", err)
		return
	}

	if len(tasks) == 0 {
		p.log.Debug("no ready tasks")
		return
	}

	p.log.Info("found ready tasks",
		"count", len(tasks),
		"tasks", formatTaskIDs(tasks),
	)

	select {
	case ch <- tasks:
	case <-ctx.Done():
	}
}

func formatTaskIDs(tasks []Task) []string {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return ids
}
