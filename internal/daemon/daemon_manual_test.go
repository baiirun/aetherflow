package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baiirun/aetherflow/internal/client"
)

func testSocketPath(prefix string) string {
	return fmt.Sprintf("/tmp/%s-%d-%d.sock", prefix, os.Getpid(), time.Now().UnixNano())
}

func waitForDaemonStatus(t *testing.T, c *client.Client, timeout time.Duration) *client.FullStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		status, err := c.StatusFull()
		if err == nil {
			return status
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon did not become ready in %v: %v", timeout, lastErr)
	return nil
}

func waitForDaemonExit(t *testing.T, done <-chan error, timeout time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon exited with error: %v", err)
		}
	case <-time.After(timeout):
		t.Fatalf("daemon did not exit within %v", timeout)
	}
}

func TestDaemonManualPolicySkipsProgRunnerCalls(t *testing.T) {
	socketPath := testSocketPath("aetherd-manual")
	var calls atomic.Int64
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls.Add(1)
		return nil, fmt.Errorf("unexpected runner call in manual mode: %s", name)
	}

	cfg := Config{
		SocketPath:        socketPath,
		Project:           "manual-test",
		PollInterval:      10 * time.Millisecond,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual,
		ReconcileInterval: DefaultReconcileInterval,
		Runner:            runner,
	}

	d := New(cfg)
	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	c := client.New(socketPath)
	status := waitForDaemonStatus(t, c, 2*time.Second)
	if status.SpawnPolicy != string(SpawnPolicyManual) {
		t.Fatalf("SpawnPolicy = %q, want %q", status.SpawnPolicy, SpawnPolicyManual)
	}

	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("runner calls = %d, want 0 in manual mode", got)
	}

	if err := c.Shutdown(); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	waitForDaemonExit(t, done, 2*time.Second)
}

func TestDaemonAutoPolicyUsesRunnerCalls(t *testing.T) {
	socketPath := testSocketPath("aetherd-auto")
	var calls atomic.Int64
	var readyCalls atomic.Int64
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls.Add(1)
		if name == "prog" && len(args) > 0 && args[0] == "ready" {
			readyCalls.Add(1)
		}
		return nil, fmt.Errorf("test runner error for %s", name)
	}

	cfg := Config{
		SocketPath:        socketPath,
		Project:           "auto-test",
		PollInterval:      10 * time.Millisecond,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyAuto,
		ReconcileInterval: DefaultReconcileInterval,
		Runner:            runner,
	}

	d := New(cfg)
	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	c := client.New(socketPath)
	waitForDaemonStatus(t, c, 2*time.Second)
	baselineReady := readyCalls.Load()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if readyCalls.Load() > baselineReady {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := readyCalls.Load(); got <= baselineReady {
		t.Fatalf("prog ready calls did not increase after baseline (baseline=%d current=%d total_runner_calls=%d)", baselineReady, got, calls.Load())
	}

	if err := c.Shutdown(); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	waitForDaemonExit(t, done, 2*time.Second)
}

func TestDaemonSecondInstanceSameSocketFailsFast(t *testing.T) {
	socketPath := testSocketPath("aetherd-manual-singleton")
	cfg := Config{
		SocketPath:        socketPath,
		PollInterval:      10 * time.Millisecond,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual,
		ReconcileInterval: DefaultReconcileInterval,
	}

	d1 := New(cfg)
	done1 := make(chan error, 1)
	go func() { done1 <- d1.Run() }()

	c := client.New(socketPath)
	waitForDaemonStatus(t, c, 2*time.Second)

	d2 := New(cfg)
	done2 := make(chan error, 1)
	go func() { done2 <- d2.Run() }()

	select {
	case err := <-done2:
		if err == nil {
			t.Fatal("expected second daemon startup to fail, got nil error")
		}
		if !strings.Contains(err.Error(), "already running") {
			t.Fatalf("expected already-running error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second daemon startup did not fail within timeout")
	}

	if err := c.Shutdown(); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	waitForDaemonExit(t, done1, 2*time.Second)
}

func TestDaemonRunRejectsAutoPolicyWithoutProject(t *testing.T) {
	cfg := Config{
		PollInterval:      10 * time.Millisecond,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyAuto,
		ReconcileInterval: DefaultReconcileInterval,
	}

	d := New(cfg)
	err := d.Run()
	if err == nil {
		t.Fatal("expected error for auto policy without project, got nil")
	}
	if !strings.Contains(err.Error(), "requires project") {
		t.Fatalf("expected requires-project error, got: %v", err)
	}
}

func TestDaemonRunRejectsUnknownSpawnPolicy(t *testing.T) {
	cfg := Config{
		PollInterval:      10 * time.Millisecond,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicy("bogus"),
		ReconcileInterval: DefaultReconcileInterval,
	}

	d := New(cfg)
	err := d.Run()
	if err == nil {
		t.Fatal("expected error for unknown spawn-policy, got nil")
	}
	if !strings.Contains(err.Error(), "unknown spawn-policy") {
		t.Fatalf("expected unknown-spawn-policy error, got: %v", err)
	}
}

func TestDaemonRunRejectsNonSocketPathCollision(t *testing.T) {
	path := fmt.Sprintf("/tmp/aetherd-collision-%d-%d", os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(path, []byte("not a socket"), 0644); err != nil {
		t.Fatalf("failed to create collision file: %v", err)
	}
	defer func() {
		_ = os.Remove(path)
	}()

	cfg := Config{
		SocketPath:        path,
		PollInterval:      10 * time.Millisecond,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual,
		ReconcileInterval: DefaultReconcileInterval,
	}

	d := New(cfg)
	err := d.Run()
	if err == nil {
		t.Fatal("expected error for non-socket path collision, got nil")
	}
	if !strings.Contains(err.Error(), "is not a unix socket") {
		t.Fatalf("expected non-socket collision error, got: %v", err)
	}
}
