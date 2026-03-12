// Package daemon implements the aetherflow daemon.
//
// The daemon is a process supervisor that:
//   - Reads task queue from prog
//   - Spawns opencode sessions as worker/planner agents
//   - Manages the agent pool (concurrency limit, stuck detection)
//   - Exposes an HTTP API for status queries and intervention
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/baiirun/aetherflow/internal/protocol"
	"github.com/baiirun/aetherflow/internal/sessions"
)

const (
	DefaultPollInterval = 10 * time.Second
)

// Daemon holds the daemon state.
type Daemon struct {
	config       Config
	httpServer   *http.Server
	poller       *Poller
	pool         *Pool
	spawns       *SpawnRegistry
	sstore       *sessions.Store
	events       *EventBuffer
	server       *exec.Cmd
	serverMu     sync.Mutex
	shutdown     chan struct{}
	shutdownOnce sync.Once
	lifeMu       sync.RWMutex
	life         protocol.DaemonLifecycleStatus
	log          *slog.Logger
}

// Response is the JSON-RPC style response envelope.
// Used by both HTTP handlers and internal handler methods.
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
		life: protocol.DaemonLifecycleStatus{
			State:       protocol.LifecycleStateStopped,
			Project:     cfg.Project,
			ServerURL:   cfg.ServerURL,
			DaemonURL:   "http://" + cfg.ListenAddr,
			SpawnPolicy: string(cfg.SpawnPolicy.Normalized()),
		},
		log: log,
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
	d.setLifecycleState(protocol.LifecycleStateStarting, "")

	// Check if another daemon is already listening on the same address.
	conn, err := net.DialTimeout("tcp", d.config.ListenAddr, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		_, port, _ := net.SplitHostPort(d.config.ListenAddr)
		d.setLifecycleState(protocol.LifecycleStateFailed, fmt.Sprintf("daemon already running on %s", d.config.ListenAddr))
		return fmt.Errorf("daemon already running on %s (run `lsof -i :%s` to identify the owner, or set listen_addr in .aetherflow.yaml to use a different port)", d.config.ListenAddr, port)
	}

	// Create HTTP server with the API handler.
	d.httpServer = &http.Server{
		Addr:              d.config.ListenAddr,
		Handler:           d.newHTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Start listener early so we can detect port conflicts before launching
	// background goroutines.
	listener, err := net.Listen("tcp", d.config.ListenAddr)
	if err != nil {
		d.setLifecycleState(protocol.LifecycleStateFailed, fmt.Sprintf("failed to listen on %s: %v", d.config.ListenAddr, err))
		return fmt.Errorf("failed to listen on %s: %w", d.config.ListenAddr, err)
	}

	daemonURL := fmt.Sprintf("http://%s", d.config.ListenAddr)
	d.log.Info("daemon started", "listen_addr", d.config.ListenAddr, "url", daemonURL)

	// Handle shutdown gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverEnv := []string{
		"AETHERFLOW_URL=" + daemonURL,
	}
	startServer := d.config.ServerStarter
	if startServer == nil {
		startServer = StartManagedServer
	}
	serverCmd, err := startServer(ctx, d.config.ServerURL, serverEnv, func(msg string, args ...any) {
		d.log.Info(msg, args...)
	})
	if err != nil {
		_ = listener.Close()
		d.setLifecycleState(protocol.LifecycleStateFailed, err.Error())
		return err
	}
	d.server = serverCmd
	if serverCmd != nil {
		go d.superviseServer(ctx)
	}
	d.setLifecycleState(protocol.LifecycleStateRunning, "")
	defer d.setLifecycleState(protocol.LifecycleStateStopped, "")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
		case <-d.shutdown:
		}
		d.setLifecycleState(protocol.LifecycleStateStopping, "")
		d.log.Info("shutting down")
		cancel()
		// Graceful HTTP shutdown with a short deadline.
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = d.httpServer.Shutdown(shutCtx)
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

	// Sweep stale data periodically (spawn entries, event buffers, session records).
	go d.sweepStale(ctx)

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

	// Serve HTTP. This blocks until the server is shut down.
	if err := d.httpServer.Serve(listener); err != http.ErrServerClosed {
		return fmt.Errorf("http server error: %w", err)
	}
	return nil
}

func (d *Daemon) superviseServer(ctx context.Context) {
	daemonURL := fmt.Sprintf("http://%s", d.config.ListenAddr)

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
			"AETHERFLOW_URL=" + daemonURL,
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

// sweepStale periodically removes stale data from all daemon subsystems:
// dead/exited spawn entries, idle event buffers, and old session records.
// All use retentionTTL (48h) so data expires together.
//
// This runs independently of the reconciler so cleanup works even when
// the reconciler is disabled (solo mode) or no project is configured.
func (d *Daemon) sweepStale(ctx context.Context) {
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
			if n := d.events.SweepIdle(); n > 0 {
				d.log.Info("event buffer sweep", "sessions_removed", n)
			}
			if d.sstore != nil {
				if n, err := d.sstore.SweepStale(retentionTTL); err != nil {
					d.log.Warn("session registry sweep failed", "error", err)
				} else if n > 0 {
					d.log.Info("session registry sweep", "records_removed", n)
				}
			}
		}
	}
}

func (d *Daemon) handleShutdown(force bool) *Response {
	d.log.Info("shutdown requested via API", "force", force)

	life := d.lifecycleStatus()
	if !force && life.ActiveSessionCount > 0 {
		result, _ := json.Marshal(protocol.StopDaemonResult{
			Outcome: protocol.StopOutcomeRefused,
			Status:  life,
			Message: fmt.Sprintf("refusing stop with %d active session(s); retry with --force after confirmation", life.ActiveSessionCount),
		})
		return &Response{Success: true, Result: result}
	}

	// Signal shutdown in background so we can send the response first.
	d.shutdownOnce.Do(func() {
		go func() {
			d.setLifecycleState(protocol.LifecycleStateStopping, "")
			close(d.shutdown)
		}()
	})

	result, _ := json.Marshal(protocol.StopDaemonResult{
		Outcome: protocol.StopOutcomeStopping,
		Status:  d.lifecycleStatus(),
		Message: "daemon stopping",
	})
	return &Response{Success: true, Result: result}
}

func (d *Daemon) handleStatusAgent(ctx context.Context, params StatusAgentParams) *Response {
	if params.AgentName == "" {
		return &Response{Success: false, Error: "agent_name is required"}
	}

	start := time.Now()
	detail, err := BuildAgentDetail(ctx, d.pool, d.spawns, d.sstore, d.events, d.config, d.config.Runner, params)
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
	status := BuildFullStatus(ctx, d.pool, d.spawns, d.sstore, d.events, d.config, d.config.Runner)

	d.log.Info("status.full",
		"agents", len(status.Agents),
		"queue", len(status.Queue),
		"errors", len(status.Errors),
		"duration", time.Since(start),
	)
	for _, e := range status.Errors {
		d.log.Warn("status.full.partial_error", "error", e)
	}

	result, err := json.Marshal(status)
	if err != nil {
		return &Response{Success: false, Error: fmt.Sprintf("marshal error: %v", err)}
	}
	return &Response{Success: true, Result: result}
}
