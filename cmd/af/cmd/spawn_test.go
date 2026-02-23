package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
)

// --- Regression tests: local spawn helpers ---

func TestBuildAgentProc(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	proc := buildAgentProc(ctx, "opencode attach http://127.0.0.1:4096", "fix the bug", "spawn-test-abc")

	// Verify command args: spawn-cmd is split by whitespace, then prompt is appended.
	if proc.Path == "" {
		t.Fatal("proc.Path should not be empty")
	}
	args := proc.Args
	// Args[0] is the command name, rest are arguments.
	if len(args) < 4 {
		t.Fatalf("args = %v, want at least 4 elements", args)
	}
	// Last arg should be the prompt.
	if args[len(args)-1] != "fix the bug" {
		t.Errorf("last arg = %q, want %q", args[len(args)-1], "fix the bug")
	}

	// Verify AETHERFLOW_AGENT_ID is set in environment.
	found := false
	for _, env := range proc.Env {
		if env == "AETHERFLOW_AGENT_ID=spawn-test-abc" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AETHERFLOW_AGENT_ID not found in proc.Env")
	}

	// Verify SysProcAttr.Setsid is set (process group isolation).
	if proc.SysProcAttr == nil {
		t.Fatal("SysProcAttr should not be nil")
	}
	if !proc.SysProcAttr.Setsid {
		t.Error("Setsid should be true for process group isolation")
	}
}

func TestBuildAgentProcPreservesParentEnv(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	proc := buildAgentProc(ctx, "echo", "test", "agent-1")

	// Should include parent environment plus AETHERFLOW_AGENT_ID.
	parentEnvCount := len(os.Environ())
	if len(proc.Env) != parentEnvCount+1 {
		t.Errorf("env count = %d, want %d (parent + AETHERFLOW_AGENT_ID)", len(proc.Env), parentEnvCount+1)
	}
}

func TestIsConnectionRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "connection refused OpError",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "connect",
					Err:     syscall.ECONNREFUSED,
				},
			},
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("something went wrong"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "net error but not connection refused",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &os.SyscallError{
					Syscall: "connect",
					Err:     syscall.ETIMEDOUT,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isConnectionRefused(tt.err)
			if got != tt.want {
				t.Errorf("isConnectionRefused() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAmbiguousProviderCreateError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: true,
		},
		{
			name: "unexpected EOF",
			err:  io.ErrUnexpectedEOF,
			want: true,
		},
		{
			name: "wrapped deadline exceeded",
			err:  errors.Join(errors.New("creating sprite"), context.DeadlineExceeded),
			want: true,
		},
		{
			name: "generic error is not ambiguous",
			err:  errors.New("bad request: invalid name"),
			want: false,
		},
		{
			name: "HTTP 400 is not ambiguous",
			err:  errors.New("create sprite failed: status 400"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isAmbiguousProviderCreateError(tt.err)
			if got != tt.want {
				t.Errorf("isAmbiguousProviderCreateError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpawnResultJSONContract(t *testing.T) {
	t.Parallel()

	result := spawnResult{
		Success: true,
		SpawnID: "spawn-ghost_wolf-a3f2",
		PID:     12345,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}
	if m["spawn_id"] != "spawn-ghost_wolf-a3f2" {
		t.Errorf("spawn_id = %v, want %q", m["spawn_id"], "spawn-ghost_wolf-a3f2")
	}
	if m["pid"] != float64(12345) {
		t.Errorf("pid = %v, want 12345", m["pid"])
	}
}

func TestSpawnErrorResultJSONContract(t *testing.T) {
	t.Parallel()

	result := spawnErrorResult{
		Success: false,
		Code:    "PROVIDER_CREATE_ERROR",
		SpawnID: "spawn-ghost_wolf-a3f2",
		Error:   "sprites create failed: status 500",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if m["success"] != false {
		t.Errorf("success = %v, want false", m["success"])
	}
	if m["code"] != "PROVIDER_CREATE_ERROR" {
		t.Errorf("code = %v, want %q", m["code"], "PROVIDER_CREATE_ERROR")
	}
	if m["error"] != "sprites create failed: status 500" {
		t.Errorf("error = %v, want %q", m["error"], "sprites create failed: status 500")
	}
}

func TestSpawnErrorResultOmitsEmptySpawnID(t *testing.T) {
	t.Parallel()

	result := spawnErrorResult{
		Success: false,
		Code:    "MISSING_TOKEN",
		Error:   "token required",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	if strings.Contains(string(data), "spawn_id") {
		t.Error("spawn_id should be omitted when empty (omitempty tag)")
	}
}

func TestNewSpawnIDFormat(t *testing.T) {
	t.Parallel()

	id := newSpawnID()

	if !strings.HasPrefix(id, "spawn-") {
		t.Errorf("spawn ID = %q, want prefix %q", id, "spawn-")
	}

	// Should have format: spawn-<name>-<hex>
	parts := strings.Split(id, "-")
	// At least: "spawn", adjective, noun, hex suffix
	if len(parts) < 3 {
		t.Errorf("spawn ID = %q, expected at least 3 parts separated by hyphens", id)
	}
}

func TestNewRequestIDFormat(t *testing.T) {
	t.Parallel()

	id, err := newRequestID()
	if err != nil {
		t.Fatalf("newRequestID() error = %v", err)
	}

	if !strings.HasPrefix(id, "req-") {
		t.Errorf("request ID = %q, want prefix %q", id, "req-")
	}

	// Should be "req-" + 16 hex chars (8 bytes).
	hexPart := strings.TrimPrefix(id, "req-")
	if len(hexPart) != 16 {
		t.Errorf("hex part len = %d, want 16", len(hexPart))
	}
}
