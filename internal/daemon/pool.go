package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

// AgentState is the lifecycle state of a pool agent.
type AgentState string

const (
	AgentRunning AgentState = "running"
	AgentExited  AgentState = "exited"
)

// Agent tracks a spawned agent process in the pool.
type Agent struct {
	ID        protocol.AgentID `json:"id"`
	TaskID    string           `json:"task_id"`
	Role      Role             `json:"role"`
	PID       int              `json:"pid"`
	SpawnTime time.Time        `json:"spawn_time"`
	State     AgentState       `json:"state"`
	ExitCode  int              `json:"exit_code,omitempty"`
}

// Process is the handle to a spawned agent process.
// This is the interface the pool uses to wait on agents.
type Process interface {
	// Wait blocks until the process exits and returns the exit error (nil for success).
	Wait() error
	// PID returns the OS process ID.
	PID() int
}

// ProcessStarter spawns a long-running agent process.
// This is the seam for testing — swap with a fake that returns immediately.
type ProcessStarter func(ctx context.Context, spawnCmd string, env []string) (Process, error)

// execProcess wraps *exec.Cmd to implement Process.
type execProcess struct {
	cmd *exec.Cmd
}

func (p *execProcess) Wait() error { return p.cmd.Wait() }
func (p *execProcess) PID() int    { return p.cmd.Process.Pid }

// ExecProcessStarter spawns a real OS process.
func ExecProcessStarter(ctx context.Context, spawnCmd string, env []string) (Process, error) {
	parts := strings.Fields(spawnCmd)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty spawn command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %q: %w", spawnCmd, err)
	}

	return &execProcess{cmd: cmd}, nil
}

// Pool manages a fixed number of agent slots.
type Pool struct {
	mu      sync.RWMutex
	agents  map[string]*Agent // keyed by task ID
	retries map[string]int    // crash count per task ID
	names   *protocol.NameGenerator
	config  Config
	runner  CommandRunner
	starter ProcessStarter
	log     *slog.Logger
	ctx     context.Context // stored for respawn goroutines
}

// NewPool creates a pool with the given configuration.
func NewPool(cfg Config, runner CommandRunner, starter ProcessStarter, log *slog.Logger) *Pool {
	if runner == nil {
		runner = ExecCommandRunner
	}
	if starter == nil {
		starter = ExecProcessStarter
	}

	return &Pool{
		agents:  make(map[string]*Agent),
		retries: make(map[string]int),
		names:   protocol.NewNameGenerator(),
		config:  cfg,
		runner:  runner,
		starter: starter,
		log:     log,
	}
}

// Run consumes tasks from the channel and schedules them onto free slots.
// It blocks until the channel is closed (context cancelled).
func (p *Pool) Run(ctx context.Context, taskCh <-chan []Task) {
	p.ctx = ctx
	p.log.Info("pool started", "pool_size", p.config.PoolSize)

	for {
		select {
		case <-ctx.Done():
			p.log.Info("pool stopped")
			return
		case tasks, ok := <-taskCh:
			if !ok {
				p.log.Info("pool stopped, task channel closed")
				return
			}
			p.schedule(ctx, tasks)
		}
	}
}

// schedule assigns ready tasks to free slots.
func (p *Pool) schedule(ctx context.Context, tasks []Task) {
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}

		p.mu.RLock()
		_, alreadyRunning := p.agents[task.ID]
		count := p.runningCount()
		p.mu.RUnlock()

		if alreadyRunning {
			continue
		}

		if count >= p.config.PoolSize {
			p.log.Debug("pool full, skipping remaining tasks",
				"running", count,
				"pool_size", p.config.PoolSize,
			)
			return
		}

		p.spawn(ctx, task)
	}
}

// spawn claims a task in prog and launches an agent process.
func (p *Pool) spawn(ctx context.Context, task Task) {
	// Claim the task in prog.
	_, err := p.runner(ctx, "prog", "start", task.ID, "-p", p.config.Project)
	if err != nil {
		p.log.Error("failed to claim task",
			"task_id", task.ID,
			"error", err,
		)
		return
	}

	// Infer role (MVP: always worker).
	meta, err := FetchTaskMeta(ctx, task.ID, p.config.Project, p.runner)
	if err != nil {
		p.log.Error("failed to fetch task metadata",
			"task_id", task.ID,
			"error", err,
		)
		return
	}
	role := InferRole(meta)

	// Generate agent name and build env.
	agentID := p.names.Generate()
	env := []string{
		"AETHERFLOW_TASK_ID=" + task.ID,
		"AETHERFLOW_ROLE=" + string(role),
		"AETHERFLOW_AGENT_ID=" + agentID.String(),
		"AETHERFLOW_PROJECT=" + p.config.Project,
	}

	proc, err := p.starter(ctx, p.config.SpawnCmd, env)
	if err != nil {
		p.log.Error("failed to spawn agent",
			"task_id", task.ID,
			"agent_id", agentID,
			"error", err,
		)
		p.names.Release(agentID)
		return
	}

	agent := &Agent{
		ID:        agentID,
		TaskID:    task.ID,
		Role:      role,
		PID:       proc.PID(),
		SpawnTime: time.Now(),
		State:     AgentRunning,
	}

	p.mu.Lock()
	p.agents[task.ID] = agent
	p.mu.Unlock()

	p.log.Info("agent spawned",
		"agent_id", agentID,
		"task_id", task.ID,
		"role", role,
		"pid", proc.PID(),
	)

	// Wait for process exit in background.
	go p.reap(agent, proc)
}

// reap waits for a process to exit, frees the slot, and respawns on crash.
func (p *Pool) reap(agent *Agent, proc Process) {
	err := proc.Wait()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	duration := time.Since(agent.SpawnTime).Round(time.Second)

	// Single lock to update all state atomically: remove agent, update retries.
	p.mu.Lock()
	agent.State = AgentExited
	agent.ExitCode = exitCode
	delete(p.agents, agent.TaskID)
	p.names.Release(agent.ID)

	if err == nil {
		// Clean exit — clear retry count.
		delete(p.retries, agent.TaskID)
	} else {
		// Crash — bump retry counter.
		p.retries[agent.TaskID]++
	}
	attempts := p.retries[agent.TaskID]
	p.mu.Unlock()

	// Clean exit — agent finished normally.
	if err == nil {
		p.log.Info("agent exited cleanly",
			"agent_id", agent.ID,
			"task_id", agent.TaskID,
			"pid", agent.PID,
			"duration", duration,
		)
		return
	}

	// Crash — decide whether to respawn.

	if attempts > p.config.MaxRetries {
		p.log.Error("agent crashed, max retries exhausted",
			"agent_id", agent.ID,
			"task_id", agent.TaskID,
			"pid", agent.PID,
			"exit_code", exitCode,
			"attempts", attempts,
			"max_retries", p.config.MaxRetries,
			"duration", duration,
		)
		return
	}

	p.log.Warn("agent crashed, respawning",
		"agent_id", agent.ID,
		"task_id", agent.TaskID,
		"pid", agent.PID,
		"exit_code", exitCode,
		"attempt", attempts,
		"max_retries", p.config.MaxRetries,
		"duration", duration,
	)

	// Respawn on the same task. The task is already in_progress in prog,
	// so we skip prog start and go straight to spawning.
	p.respawn(agent.TaskID, agent.Role)
}

// respawn launches a new agent for a task that's already in_progress.
func (p *Pool) respawn(taskID string, role Role) {
	if p.ctx.Err() != nil {
		return
	}

	agentID := p.names.Generate()
	env := []string{
		"AETHERFLOW_TASK_ID=" + taskID,
		"AETHERFLOW_ROLE=" + string(role),
		"AETHERFLOW_AGENT_ID=" + agentID.String(),
		"AETHERFLOW_PROJECT=" + p.config.Project,
	}

	proc, err := p.starter(p.ctx, p.config.SpawnCmd, env)
	if err != nil {
		p.log.Error("failed to respawn agent",
			"task_id", taskID,
			"agent_id", agentID,
			"error", err,
		)
		p.names.Release(agentID)
		return
	}

	agent := &Agent{
		ID:        agentID,
		TaskID:    taskID,
		Role:      role,
		PID:       proc.PID(),
		SpawnTime: time.Now(),
		State:     AgentRunning,
	}

	p.mu.Lock()
	p.agents[taskID] = agent
	p.mu.Unlock()

	p.log.Info("agent respawned",
		"agent_id", agentID,
		"task_id", taskID,
		"role", role,
		"pid", proc.PID(),
	)

	go p.reap(agent, proc)
}

// runningCount returns the number of currently running agents.
// Caller must hold at least a read lock.
func (p *Pool) runningCount() int {
	return len(p.agents)
}

// Status returns the current pool state for the status RPC.
func (p *Pool) Status() []Agent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	agents := make([]Agent, 0, len(p.agents))
	for _, a := range p.agents {
		agents = append(agents, *a)
	}
	return agents
}

// PoolState is the persisted pool state for daemon restart recovery.
type PoolState struct {
	Agents []Agent `json:"agents"`
}

// SaveState writes the pool state to a file.
func (p *Pool) SaveState(path string) error {
	p.mu.RLock()
	state := PoolState{Agents: p.Status()}
	p.mu.RUnlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling pool state: %w", err)
	}

	// Write to temp file and rename for atomicity.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pool-state-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("writing pool state: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("renaming pool state: %w", err)
	}

	return nil
}

// LoadState reads persisted pool state. Returns empty state if file doesn't exist.
func LoadState(path string) (PoolState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PoolState{}, nil
		}
		return PoolState{}, fmt.Errorf("reading pool state: %w", err)
	}

	var state PoolState
	if err := json.Unmarshal(data, &state); err != nil {
		return PoolState{}, fmt.Errorf("parsing pool state: %w", err)
	}

	return state, nil
}
