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
	"os/signal"
	"syscall"
	"time"
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
	if cfg.Project != "" {
		poller = NewPoller(cfg.Project, cfg.PollInterval, cfg.Runner, log)
		pool = NewPool(cfg, cfg.Runner, cfg.Starter, log)
	}

	return &Daemon{
		config:   cfg,
		poller:   poller,
		pool:     pool,
		shutdown: make(chan struct{}),
		log:      log,
	}
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run() error {
	// Remove stale socket
	os.Remove(d.config.SocketPath)

	listener, err := net.Listen("unix", d.config.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", d.config.SocketPath, err)
	}
	// Restrict socket to owner only — prevents other local users from
	// issuing RPC commands (especially shutdown) to this daemon.
	if err := os.Chmod(d.config.SocketPath, 0700); err != nil {
		listener.Close()
		return fmt.Errorf("failed to set socket permissions on %s: %w", d.config.SocketPath, err)
	}
	d.listener = listener

	d.log.Info("daemon started", "socket", d.config.SocketPath)

	// Handle shutdown gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
		case <-d.shutdown:
		}
		d.log.Info("shutting down")
		cancel()
		listener.Close()
	}()

	// Start poll loop and pool if a project is configured.
	if d.poller != nil && d.pool != nil {
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

func (d *Daemon) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

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
	detail, err := BuildAgentDetail(ctx, d.pool, d.config, d.config.Runner, params)
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
	status := BuildFullStatus(ctx, d.pool, d.config, d.config.Runner)

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
