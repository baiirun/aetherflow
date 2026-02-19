// Package daemon implements the aetherflow daemon.
//
// The daemon is a process supervisor that:
//   - Reads task queue from prog
//   - Spawns opencode sessions as worker/planner agents
//   - Manages the agent pool (concurrency limit, stuck detection)
//   - Exposes a Unix socket for status queries and intervention
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/baiirun/aetherflow/internal/sessions"
)

const (
	DefaultPollInterval = 10 * time.Second
)

// Daemon holds the daemon state.
type Daemon struct {
	config   Config
	listener net.Listener
	poller   *Poller
	pool     *Pool
	spawns   *SpawnRegistry
	sstore   *sessions.Store
	events   *EventBuffer
	server   *exec.Cmd
	serverMu sync.Mutex
	shutdown chan struct{}
	log      *slog.Logger
}

// Request is the JSON-RPC style request envelope.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the JSON-RPC style response envelope.
type Response struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// New creates a new daemon with the given config.
// Call cfg.ApplyDefaults() and cfg.Validate() before passing to New.
func New(cfg Config) *Daemon {
	cfg.ApplyDefaults()

	// Default Runner and Starter to real implementations so all consumers
	// (pool, poller, status handlers) use the same runner without nil checks.
	if cfg.Runner == nil {
		cfg.Runner = ExecCommandRunner
	}
	if cfg.Starter == nil {
		cfg.Starter = ExecProcessStarter
	}

	log := cfg.Logger

	var poller *Poller
	var pool *Pool
	store, storeErr := sessions.Open(cfg.SessionDir)
	if storeErr != nil && log != nil {
		log.Warn("session registry unavailable", "error", storeErr)
	}
	if cfg.Project != "" {
		poller = NewPoller(cfg.Project, cfg.PollInterval, cfg.Runner, log)
		pool = NewPool(cfg, cfg.Runner, cfg.Starter, log)
		if pool != nil {
			pool.sstore = store
		}
	}

	return &Daemon{
		config:   cfg,
		poller:   poller,
		pool:     pool,
		spawns:   NewSpawnRegistry(),
		sstore:   store,
		events:   NewEventBuffer(DefaultEventBufSize),
		shutdown: make(chan struct{}),
		log:      log,
	}
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run() error {
	// Defend against callers that bypass config validation.
	// Fail fast instead of silently starting in a degraded mode.
	policy := d.config.SpawnPolicy.Normalized()
	switch policy {
	case SpawnPolicyAuto:
		if d.config.Project == "" {
			return fmt.Errorf("invalid config: spawn-policy %q requires project", SpawnPolicyAuto)
		}
		if d.poller == nil || d.pool == nil {
			return fmt.Errorf("invariant violated: spawn-policy %q requires poller and pool", SpawnPolicyAuto)
		}
	case SpawnPolicyManual:
		// valid
	default:
		return fmt.Errorf("invalid config: unknown spawn-policy %q", policy)
	}
	if (d.poller == nil) != (d.pool == nil) {
		return fmt.Errorf("invariant violated: poller and pool must be both nil or both non-nil")
	}

	conn, err := net.DialTimeout("unix", d.config.SocketPath, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("daemon already running on %s", d.config.SocketPath)
	}

	// Remove stale socket if no daemon is accepting connections.
	if info, statErr := os.Lstat(d.config.SocketPath); statErr == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("socket path exists and is not a unix socket: %s", d.config.SocketPath)
		}
		if rmErr := os.Remove(d.config.SocketPath); rmErr != nil {
			return fmt.Errorf("failed to remove stale socket %s: %w", d.config.SocketPath, rmErr)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to stat socket path %s: %w", d.config.SocketPath, statErr)
	}

	listener, err := net.Listen("unix", d.config.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", d.config.SocketPath, err)
	}
	// Restrict socket to owner only — prevents other local users from
	// issuing RPC commands (especially shutdown) to this daemon.
	if err := os.Chmod(d.config.SocketPath, 0700); err != nil {
		_ = listener.Close()
		return fmt.Errorf("failed to set socket permissions on %s: %w", d.config.SocketPath, err)
	}
	d.listener = listener

	d.log.Info("daemon started", "socket", d.config.SocketPath)

	// Handle shutdown gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverEnv := []string{
		"AETHERFLOW_SOCKET=" + d.config.SocketPath,
	}
	serverCmd, err := StartManagedServer(ctx, d.config.ServerURL, serverEnv, func(msg string, args ...any) {
		d.log.Info(msg, args...)
	})
	if err != nil {
		_ = listener.Close()
		return err
	}
	d.server = serverCmd
	if serverCmd != nil {
		go d.superviseServer(ctx)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
		case <-d.shutdown:
		}
		d.log.Info("shutting down")
		cancel()
		_ = listener.Close()
	}()

	// Start poll loop and pool if a project is configured and auto-spawn is enabled.
	if d.poller != nil && d.pool != nil {
		if !policy.AutoSchedulingEnabled() {
			d.log.Info("spawn policy manual: auto-scheduling disabled")
		} else {
			// Set pool context before launching goroutines so both Run and
			// Reclaim (which calls respawn, which uses p.ctx) are safe.
			d.pool.SetContext(ctx)

			taskCh := d.poller.Start(ctx)
			go d.pool.Run(ctx, taskCh)

			// Reclaim orphaned in_progress tasks from a previous daemon session.
			// These are tasks that were claimed in prog but whose agents died
			// when the daemon crashed or was stopped.
			//
			// Delay briefly so the poller's initial `prog ready` completes first.
			// Both hit prog's SQLite database and concurrent access during WAL
			// mode initialization causes "database is locked" errors.
			go func() {
				select {
				case <-time.After(2 * time.Second):
					d.pool.Reclaim(ctx)
				case <-ctx.Done():
				}
			}()

			// Reconcile reviewing tasks — periodically check if branches have
			// been merged to main and mark the corresponding tasks as done.
			// Skip in solo mode: solo agents merge directly and call prog done
			// themselves, so there's nothing to reconcile.
			if !d.config.Solo {
				go d.reconcileReviewing(ctx)
			}
		}
	}

	// Sweep dead spawned agents periodically.
	go d.sweepSpawns(ctx)

	// Backfill event buffer from the opencode REST API for sessions that
	// existed before this daemon started. Runs in background so it doesn't
	// block accepting connections — the daemon is usable immediately, and
	// events from the plugin will flow as soon as agents start.
	go func() {
		bctx, bcancel := context.WithTimeout(ctx, backfillTimeout)
		defer bcancel()
		api := newOpencodeClient(d.config.ServerURL)
		backfillEvents(bctx, api, d.sstore, d.events, d.log)
	}()

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				d.log.Error("accept error", "error", err)
				continue
			}
		}
		go d.handleConnection(ctx, conn)
	}
}

func (d *Daemon) superviseServer(ctx context.Context) {
	for {
		d.serverMu.Lock()
		cmd := d.server
		d.serverMu.Unlock()
		if cmd == nil {
			return
		}

		err := cmd.Wait()
		select {
		case <-ctx.Done():
			return
		default:
		}

		d.log.Warn("managed opencode server exited, restarting", "error", err)
		time.Sleep(500 * time.Millisecond)
		restartEnv := []string{
			"AETHERFLOW_SOCKET=" + d.config.SocketPath,
		}
		restarted, startErr := StartManagedServer(ctx, d.config.ServerURL, restartEnv, func(msg string, args ...any) {
			d.log.Info(msg, args...)
		})
		if startErr != nil {
			d.log.Error("failed to restart managed opencode server", "error", startErr)
			continue
		}
		if restarted == nil {
			// Existing server is already up; nothing to supervise.
			return
		}
		d.serverMu.Lock()
		d.server = restarted
		d.serverMu.Unlock()
	}
}

// sweepSpawns periodically removes dead spawned agents from the registry.
// This runs independently of the reconciler so spawn cleanup works even when
// the reconciler is disabled (solo mode) or no project is configured.
func (d *Daemon) sweepSpawns(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval) // same interval as pool sweep (pool.go)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if result := d.spawns.SweepDead(); result.Total() > 0 {
				d.log.Info("spawn sweep", "marked_exited", result.Marked, "removed", result.Removed)
			}
		}
	}
}

func (d *Daemon) handleConnection(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return // Connection closed or read error
		}

		d.log.Debug("rpc request", "method", req.Method)
		resp := d.handleRequest(ctx, &req)
		if err := encoder.Encode(resp); err != nil {
			return
		}
	}
}

func (d *Daemon) handleRequest(ctx context.Context, req *Request) *Response {
	switch req.Method {
	case "status.full":
		return d.handleStatusFull(ctx)
	case "status.agent":
		return d.handleStatusAgent(ctx, req.Params)
	case "logs.path":
		return d.handleLogsPath(req.Params)
	case "pool.drain":
		return d.handlePoolDrain()
	case "pool.pause":
		return d.handlePoolPause()
	case "pool.resume":
		return d.handlePoolResume()
	case "spawn.register":
		return d.handleSpawnRegister(req.Params)
	case "spawn.deregister":
		return d.handleSpawnDeregister(req.Params)
	case "session.event":
		return d.handleSessionEvent(req.Params)
	case "shutdown":
		return d.handleShutdown()
	default:
		return &Response{Success: false, Error: fmt.Sprintf("unknown method: %s", req.Method)}
	}
}

func (d *Daemon) handleShutdown() *Response {
	d.log.Info("shutdown requested via RPC")
	// Signal shutdown in background so we can send response first
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(d.shutdown)
	}()
	return &Response{Success: true}
}

func (d *Daemon) handleStatusAgent(ctx context.Context, rawParams json.RawMessage) *Response {
	var params StatusAgentParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("invalid params: %v", err)}
		}
	}
	if params.AgentName == "" {
		return &Response{Success: false, Error: "agent_name is required"}
	}

	start := time.Now()
	detail, err := BuildAgentDetail(ctx, d.pool, d.spawns, d.events, d.config, d.config.Runner, params)
	if err != nil {
		return &Response{Success: false, Error: err.Error()}
	}

	d.log.Info("status.agent",
		"agent", params.AgentName,
		"tool_calls", len(detail.ToolCalls),
		"errors", len(detail.Errors),
		"duration", time.Since(start),
	)

	for _, e := range detail.Errors {
		d.log.Warn("status.agent.partial_error", "agent", params.AgentName, "error", e)
	}

	result, err := json.Marshal(detail)
	if err != nil {
		return &Response{Success: false, Error: fmt.Sprintf("marshal error: %v", err)}
	}
	return &Response{Success: true, Result: result}
}

func (d *Daemon) handleStatusFull(ctx context.Context) *Response {
	start := time.Now()
	status := BuildFullStatus(ctx, d.pool, d.spawns, d.config, d.config.Runner)

	d.log.Info("status.full",
		"agents", len(status.Agents),
		"queue", len(status.Queue),
		"errors", len(status.Errors),
		"duration", time.Since(start),
	)

	result, err := json.Marshal(status)
	if err != nil {
		return &Response{Success: false, Error: fmt.Sprintf("marshal error: %v", err)}
	}
	return &Response{Success: true, Result: result}
}
