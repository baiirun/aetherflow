package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/baiirun/aetherflow/internal/protocol"
	"github.com/baiirun/aetherflow/internal/sessions"
)

// PoolMode controls pool scheduling behavior.
type PoolMode string

const (
	// PoolActive is the default mode — normal scheduling and respawning.
	PoolActive PoolMode = "active"

	// PoolDraining stops scheduling new tasks from the queue but lets
	// current agents run to completion. Crash respawns are still allowed
	// since those tasks are already claimed in prog.
	PoolDraining PoolMode = "draining"

	// PoolPaused stops both new scheduling and crash respawns.
	// Existing agents continue running but won't be restarted on crash.
	PoolPaused PoolMode = "paused"
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
	SessionID string           `json:"session_id,omitempty"`
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
// The prompt is the rendered role prompt passed as the message argument to the spawn command.
// agentID is set as the AETHERFLOW_AGENT_ID environment variable on the spawned process
// so plugins inside the agent session can identify which agent they belong to.
// stdout receives the process's standard output (typically a log file for JSONL capture).
// This is the seam for testing — swap with a fake that returns immediately.
type ProcessStarter func(ctx context.Context, spawnCmd string, prompt string, agentID string, stdout io.Writer) (Process, error)

// execProcess wraps *exec.Cmd to implement Process.
type execProcess struct {
	cmd *exec.Cmd
}

func (p *execProcess) Wait() error { return p.cmd.Wait() }
func (p *execProcess) PID() int    { return p.cmd.Process.Pid }

// ExecProcessStarter spawns a real OS process.
// The prompt is appended as the final argument to the spawn command,
// e.g. "opencode run --format json" becomes ["opencode", "run", "--format", "json", "<prompt>"].
// agentID is exposed as the AETHERFLOW_AGENT_ID environment variable so plugins
// running inside the agent session can identify which agent they belong to.
// stdout receives the process's standard output (typically a JSONL log file).
func ExecProcessStarter(ctx context.Context, spawnCmd string, prompt string, agentID string, stdout io.Writer) (Process, error) {
	parts := strings.Fields(spawnCmd)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty spawn command")
	}

	parts = append(parts, prompt)
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Env = append(os.Environ(), "AETHERFLOW_AGENT_ID="+agentID)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Own process group so terminal signals don't propagate to daemon
	}
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %q: %w", spawnCmd, err)
	}

	return &execProcess{cmd: cmd}, nil
}

// logFilePath returns the path for a task's JSONL log file.
// taskID is sanitized with filepath.Base to prevent path traversal.
func logFilePath(logDir, taskID string) string {
	return filepath.Join(logDir, filepath.Base(taskID)+".jsonl")
}

// openLogFile creates the log directory if needed and opens the log file for appending.
// Log files are owner-only (0600) since agent stdout may contain sensitive data.
//
// Note: writes are not fsynced — a daemon crash may lose buffered JSONL lines.
// This is acceptable for observability data; the agent process is unaffected.
func openLogFile(logDir, taskID string) (*os.File, error) {
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, fmt.Errorf("creating log directory %s: %w", logDir, err)
	}
	path := logFilePath(logDir, taskID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening log file %s: %w", path, err)
	}
	return f, nil
}

// Pool manages a fixed number of agent slots.
type Pool struct {
	mu      sync.RWMutex
	mode    PoolMode          // controls scheduling behavior
	agents  map[string]*Agent // keyed by task ID
	retries map[string]int    // crash count per task ID
	names   *protocol.NameGenerator
	config  Config
	logDir  string // absolute path to JSONL log directory
	runner  CommandRunner
	starter ProcessStarter
	sstore  *sessions.Store
	work    WorkSource
	log     *slog.Logger
	ctx     context.Context // stored for respawn goroutines

	// pidAlive checks whether a process with the given PID is still running.
	// Defaults to the real syscall check; overridden in tests.
	pidAlive func(int) bool
}

// defaultPIDAlive checks process liveness via kill(pid, 0).
// Returns false when the process does not exist (ESRCH).
// Returns true for any other case (alive, or permission denied which
// means the process exists but belongs to another user).
func defaultPIDAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// ESRCH = no such process — the only signal that the PID is gone.
	return err != syscall.ESRCH
}

const sweepInterval = 30 * time.Second

// NewPool creates a pool with the given configuration.
func NewPool(cfg Config, runner CommandRunner, starter ProcessStarter, log *slog.Logger) *Pool {
	if runner == nil {
		runner = ExecCommandRunner
	}
	if starter == nil {
		starter = ExecProcessStarter
	}

	return &Pool{
		mode:     PoolActive,
		agents:   make(map[string]*Agent),
		retries:  make(map[string]int),
		names:    protocol.NewNameGenerator(),
		config:   cfg,
		logDir:   cfg.LogDir,
		runner:   runner,
		starter:  starter,
		sstore:   nil,
		work:     NewProgWorkSource(runner),
		log:      log,
		pidAlive: defaultPIDAlive,
	}
}

// SetContext sets the pool's context for use by respawn goroutines.
// Must be called before Reclaim or any operation that triggers respawn
// outside of the Run loop. Run also sets the context, but calling
// SetContext first avoids a race when Run and Reclaim start concurrently.
func (p *Pool) SetContext(ctx context.Context) {
	p.ctx = ctx
}

// Run consumes tasks from the channel and schedules them onto free slots.
// It blocks until the channel is closed (context cancelled).
// Caller must call SetContext before Run if Reclaim will run concurrently.
func (p *Pool) Run(ctx context.Context, taskCh <-chan []Task) {
	// If SetContext wasn't called (standalone usage, tests), set it now.
	// When SetContext was called first (daemon startup), this is a no-op.
	if p.ctx == nil {
		p.ctx = ctx
	}
	p.log.Info("pool started", "pool_size", p.config.PoolSize)

	sweepTicker := time.NewTicker(sweepInterval)
	defer sweepTicker.Stop()

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
		case <-sweepTicker.C:
			p.sweepDead()
		}
	}
}

// schedule assigns ready tasks to free slots.
// Skips all scheduling when the pool is draining or paused.
func (p *Pool) schedule(ctx context.Context, tasks []Task) {
	p.mu.RLock()
	mode := p.mode
	p.mu.RUnlock()

	if mode != PoolActive {
		p.log.Debug("schedule skipped, pool not active", "mode", mode, "task_count", len(tasks))
		return
	}

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
//
// The sequence is: prep (fetch metadata, render prompt, open log) → claim → spawn.
// All fallible prep happens before claiming so a failure doesn't orphan
// the task in "in_progress" state with no agent.
func (p *Pool) spawn(ctx context.Context, task Task) {
	// Prep: fetch metadata and infer role before claiming.
	meta, err := p.work.GetMeta(ctx, task.ID, p.config.Project)
	if err != nil {
		p.log.Error("failed to fetch task metadata",
			"task_id", task.ID,
			"error", err,
		)
		return
	}
	role := InferRole(meta)

	// Prep: render the role prompt with the task ID baked in.
	prompt, err := RenderPrompt(p.config.PromptDir, role, task.ID, p.config.Solo)
	if err != nil {
		p.log.Error("failed to render prompt",
			"task_id", task.ID,
			"role", role,
			"error", err,
		)
		return
	}

	// Prep: open log file before claiming task. If this fails, the task stays
	// in the queue rather than being orphaned in in_progress with no agent.
	logFile, err := openLogFile(p.logDir, task.ID)
	if err != nil {
		p.log.Error("failed to open log file",
			"task_id", task.ID,
			"error", err,
		)
		return
	}

	// Claim the task in prog. This is the point of no return — after this,
	// the task is in_progress and we must either spawn an agent or leave it
	// for manual recovery.
	err = p.work.Claim(ctx, task.ID, p.config.Project)
	if err != nil {
		_ = logFile.Close()
		p.log.Error("failed to claim task",
			"task_id", task.ID,
			"error", err,
		)
		return
	}

	agentID := p.names.Generate()

	launchCmd := EnsureAttachSpawnCmd(p.config.SpawnCmd, p.config.ServerURL)
	proc, err := p.starter(ctx, launchCmd, prompt, string(agentID), logFile)
	if err != nil {
		_ = logFile.Close()
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

	go p.captureSessionFromLog(ctx, task.ID, string(agentID), logFilePath(p.logDir, task.ID), sessions.Record{
		ServerRef: p.config.ServerURL,
		Directory: "",
		Project:   p.config.Project,
		Origin:    sessions.OriginPool,
		WorkRef:   task.ID,
		AgentID:   string(agentID),
		Status:    sessions.StatusActive,
	})

	// Wait for process exit in background.
	go p.reap(agent, proc, logFile)
}

// reap waits for a process to exit, frees the slot, and respawns on crash.
// cleanup is closed after the process exits (typically the log file).
func (p *Pool) reap(agent *Agent, proc Process, cleanup io.Closer) {
	err := proc.Wait()
	if closeErr := cleanup.Close(); closeErr != nil {
		p.log.Warn("failed to close log file",
			"agent_id", agent.ID,
			"task_id", agent.TaskID,
			"error", closeErr,
		)
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	duration := time.Since(agent.SpawnTime).Round(time.Second)

	var targetStatus sessions.Status
	var sessionID string

	// Single lock to update all pool state atomically.
	p.mu.Lock()
	agent.State = AgentExited
	agent.ExitCode = exitCode
	sessionID = agent.SessionID
	delete(p.agents, agent.TaskID)
	p.names.Release(agent.ID)

	if err == nil {
		// Clean exit — clear retry count.
		delete(p.retries, agent.TaskID)
		targetStatus = sessions.StatusIdle
	} else {
		// Crash — bump retry counter.
		p.retries[agent.TaskID]++
		targetStatus = sessions.StatusTerminated
	}
	attempts := p.retries[agent.TaskID]
	p.mu.Unlock()

	p.updateSessionStatus(sessionID, sessions.OriginPool, agent.TaskID, targetStatus)

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
	// respawn() opens its own log file — no handle passed across.
	p.respawn(agent.TaskID, agent.Role)
}

// respawn launches a new agent for a task that's already in_progress.
// Respawns are blocked when the pool is paused. In draining mode,
// respawns are allowed because the task is already claimed in prog
// and leaving it without an agent would orphan it.
func (p *Pool) respawn(taskID string, role Role) {
	if p.ctx.Err() != nil {
		return
	}

	p.mu.RLock()
	mode := p.mode
	p.mu.RUnlock()

	if mode == PoolPaused {
		p.log.Info("respawn skipped, pool is paused",
			"task_id", taskID,
			"role", role,
		)
		return
	}

	// Re-render the prompt from disk. This intentionally re-reads the template
	// so prompt changes take effect on respawn without daemon restart.
	prompt, err := RenderPrompt(p.config.PromptDir, role, taskID, p.config.Solo)
	if err != nil {
		p.log.Error("failed to render prompt for respawn",
			"task_id", taskID,
			"role", role,
			"error", err,
		)
		return
	}

	// Reopen the same log file in append mode.
	// Uses openLogFile (with MkdirAll) rather than assuming the directory exists,
	// so respawn is resilient to the log dir being removed between spawns.
	logFile, err := openLogFile(p.logDir, taskID)
	if err != nil {
		p.log.Error("failed to open log file for respawn",
			"task_id", taskID,
			"error", err,
		)
		return
	}

	agentID := p.names.Generate()

	launchCmd := EnsureAttachSpawnCmd(p.config.SpawnCmd, p.config.ServerURL)
	proc, err := p.starter(p.ctx, launchCmd, prompt, string(agentID), logFile)
	if err != nil {
		_ = logFile.Close()
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

	go p.captureSessionFromLog(p.ctx, taskID, string(agentID), logFilePath(p.logDir, taskID), sessions.Record{
		ServerRef: p.config.ServerURL,
		Directory: "",
		Project:   p.config.Project,
		Origin:    sessions.OriginPool,
		WorkRef:   taskID,
		AgentID:   string(agentID),
		Status:    sessions.StatusActive,
	})

	go p.reap(agent, proc, logFile)
}

func (p *Pool) captureSessionFromLog(ctx context.Context, taskID, agentID, path string, base sessions.Record) {
	if p.sstore == nil {
		return
	}

	const maxAttempts = 40
	var firstErr error
	for i := 0; i < maxAttempts; i++ {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}

		p.mu.RLock()
		a, ok := p.agents[taskID]
		alive := ok && string(a.ID) == agentID
		p.mu.RUnlock()
		if !alive {
			return
		}

		sid, err := ParseSessionID(context.Background(), path)
		if err == nil && sid != "" {
			base.SessionID = sid
			base.LastSeenAt = time.Now()
			if err := p.sstore.Upsert(base); err != nil {
				p.log.Warn("failed to persist session record", "path", path, "error", err)
				return
			}

			p.mu.Lock()
			if cur, ok := p.agents[taskID]; ok && string(cur.ID) == agentID {
				cur.SessionID = sid
			}
			p.mu.Unlock()
			return
		}
		if err != nil && firstErr == nil {
			firstErr = err
			p.log.Debug("session capture retry",
				"task_id", taskID,
				"agent_id", agentID,
				"path", path,
				"error", err,
			)
		}
		time.Sleep(250 * time.Millisecond)
	}
	if firstErr != nil {
		p.log.Warn("session capture exhausted retries",
			"task_id", taskID,
			"agent_id", agentID,
			"path", path,
			"attempts", maxAttempts,
			"first_error", firstErr,
		)
	}
}

func (p *Pool) updateSessionStatus(sessionID string, origin sessions.OriginType, workRef string, status sessions.Status) {
	if p.sstore == nil {
		return
	}
	if sessionID != "" {
		if changed, err := p.sstore.SetStatusBySession(p.config.ServerURL, sessionID, status); err != nil {
			p.log.Warn("failed to update session status by key", "session_id", sessionID, "status", status, "error", err)
		} else if changed {
			return
		}
	}
	if workRef == "" {
		return
	}
	if changed, err := p.sstore.SetStatusByWorkRef(origin, workRef, status); err != nil {
		p.log.Warn("failed to update session status", "work_ref", workRef, "status", status, "error", err)
	} else if !changed {
		p.log.Debug("session status update skipped (record not found yet)", "work_ref", workRef, "status", status)
	}
}

// runningCount returns the number of currently running agents.
// Caller must hold at least a read lock.
func (p *Pool) runningCount() int {
	return len(p.agents)
}

// sweepDead removes agents whose OS process has exited but whose reap
// goroutine is stuck on Wait(). This is a safety net — normally reap()
// handles cleanup, but when Wait() hangs (observed with Setsid session
// leaders), the agent slot is leaked. The sweep checks PID liveness via
// kill(pid, 0) and force-removes dead agents.
//
// The orphaned reap goroutine for each swept agent will leak (it's blocked
// on Wait() forever). This is acceptable: one goroutine per dead agent,
// and dead agents are rare.
func (p *Pool) sweepDead() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for taskID, agent := range p.agents {
		if p.pidAlive(agent.PID) {
			continue
		}

		p.log.Warn("sweep: removing dead agent (PID gone, Wait hung)",
			"agent_id", agent.ID,
			"task_id", taskID,
			"pid", agent.PID,
			"uptime", time.Since(agent.SpawnTime).Round(time.Second),
		)

		delete(p.agents, taskID)
		p.names.Release(agent.ID)
	}
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

// Mode returns the current pool mode.
func (p *Pool) Mode() PoolMode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.mode
}

// Drain transitions the pool to draining mode. New tasks from the queue
// are not scheduled, but current agents run to completion and crash
// respawns are still allowed (the task is already claimed in prog).
func (p *Pool) Drain() {
	p.mu.Lock()
	defer p.mu.Unlock()
	prev := p.mode
	p.mode = PoolDraining
	p.log.Info("pool mode changed", "from", prev, "to", PoolDraining)
}

// Pause transitions the pool to paused mode. No new scheduling and
// no crash respawns. Existing agents continue running.
func (p *Pool) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	prev := p.mode
	p.mode = PoolPaused
	p.log.Info("pool mode changed", "from", prev, "to", PoolPaused)
}

// Resume transitions the pool back to active mode from any state.
// Note: tasks dropped during drain/pause are not retroactively scheduled;
// they will be picked up on the next poll cycle.
func (p *Pool) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	prev := p.mode
	p.mode = PoolActive
	p.log.Info("pool mode changed", "from", prev, "to", PoolActive)
}
