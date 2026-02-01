package main

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

	"github.com/geobrowser/aetherflow/internal/protocol"
)

const DefaultSocketPath = "/tmp/aetherd.sock"

// Daemon holds the daemon state.
type Daemon struct {
	socketPath string
	listener   net.Listener
	agents     map[protocol.AgentID]*protocol.AgentInfo
	nameGen    *protocol.NameGenerator
	mu         sync.RWMutex
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

func main() {
	socketPath := DefaultSocketPath
	if len(os.Args) > 1 {
		socketPath = os.Args[1]
	}

	d := &Daemon{
		socketPath: socketPath,
		agents:     make(map[protocol.AgentID]*protocol.AgentInfo),
		nameGen:    protocol.NewNameGenerator(),
	}

	if err := d.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func (d *Daemon) Run() error {
	// Remove stale socket
	os.Remove(d.socketPath)

	listener, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", d.socketPath, err)
	}
	d.listener = listener

	fmt.Printf("aetherd listening on %s\n", d.socketPath)

	// Handle shutdown gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
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
	case "register":
		return d.handleRegister()
	case "unregister":
		return d.handleUnregister(req.Params)
	case "list_agents":
		return d.handleListAgents()
	case "status":
		return d.handleStatus()
	default:
		return &Response{Success: false, Error: fmt.Sprintf("unknown method: %s", req.Method)}
	}
}

func (d *Daemon) handleRegister() *Response {
	d.mu.Lock()
	defer d.mu.Unlock()

	agentID := d.nameGen.Generate()

	info := &protocol.AgentInfo{
		ID:           agentID,
		State:        protocol.StateIdle,
		RegisteredAt: time.Now().UnixMilli(),
	}
	d.agents[agentID] = info

	resp := protocol.RegistrationResponse{AgentID: agentID}
	result, _ := json.Marshal(resp)

	fmt.Printf("+ %s\n", agentID)
	return &Response{Success: true, Result: result}
}

type unregisterParams struct {
	AgentID protocol.AgentID `json:"agent_id"`
}

func (d *Daemon) handleUnregister(params json.RawMessage) *Response {
	var p unregisterParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Response{Success: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.agents[p.AgentID]; !ok {
		return &Response{Success: false, Error: fmt.Sprintf("agent not found: %s", p.AgentID)}
	}

	delete(d.agents, p.AgentID)
	d.nameGen.Release(p.AgentID)

	fmt.Printf("- %s\n", p.AgentID)
	return &Response{Success: true}
}

func (d *Daemon) handleListAgents() *Response {
	d.mu.RLock()
	defer d.mu.RUnlock()

	agents := make([]*protocol.AgentInfo, 0, len(d.agents))
	for _, info := range d.agents {
		agents = append(agents, info)
	}

	result, _ := json.Marshal(agents)
	return &Response{Success: true, Result: result}
}

func (d *Daemon) handleStatus() *Response {
	d.mu.RLock()
	defer d.mu.RUnlock()

	status := map[string]any{
		"agents": len(d.agents),
		"socket": d.socketPath,
	}

	result, _ := json.Marshal(status)
	return &Response{Success: true, Result: result}
}
