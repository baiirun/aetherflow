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
	DefaultSocketPath   = "/tmp/aetherd.sock"
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
		taskCh := d.poller.Start(ctx)
		go d.pool.Run(ctx, taskCh)
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
		go d.handleConnection(conn)
	}
}

func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return // Connection closed or invalid JSON
		}

		resp := d.handleRequest(&req)
		if err := encoder.Encode(resp); err != nil {
			return
		}
	}
}

func (d *Daemon) handleRequest(req *Request) *Response {
	switch req.Method {
	case "status":
		return d.handleStatus()
	case "shutdown":
		return d.handleShutdown()
	default:
		return &Response{Success: false, Error: fmt.Sprintf("unknown method: %s", req.Method)}
	}
}

func (d *Daemon) handleShutdown() *Response {
	// Signal shutdown in background so we can send response first
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(d.shutdown)
	}()
	return &Response{Success: true}
}

func (d *Daemon) handleStatus() *Response {
	status := map[string]any{
		"socket":    d.config.SocketPath,
		"project":   d.config.Project,
		"pool_size": d.config.PoolSize,
	}

	if d.pool != nil {
		agents := d.pool.Status()
		status["agents"] = agents
		status["agents_running"] = len(agents)
	}

	result, _ := json.Marshal(status)
	return &Response{Success: true, Result: result}
}
