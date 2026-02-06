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
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const DefaultSocketPath = "/tmp/aetherd.sock"

// Daemon holds the daemon state.
type Daemon struct {
	socketPath string
	listener   net.Listener
	mu         sync.RWMutex
	shutdown   chan struct{}
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

// New creates a new daemon.
func New(socketPath string) *Daemon {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	return &Daemon{
		socketPath: socketPath,
		shutdown:   make(chan struct{}),
	}
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run() error {
	// Remove stale socket
	os.Remove(d.socketPath)

	listener, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", d.socketPath, err)
	}
	d.listener = listener

	fmt.Printf("listening on %s\n", d.socketPath)

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
		fmt.Println("\nshutting down...")
		cancel()
		listener.Close()
	}()

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				fmt.Fprintf(os.Stderr, "accept error: %v\n", err)
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
	d.mu.RLock()
	defer d.mu.RUnlock()

	status := map[string]any{
		"socket": d.socketPath,
	}

	result, _ := json.Marshal(status)
	return &Response{Success: true, Result: result}
}
