package daemon

import (
	"context"
	"fmt"
	"os"
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
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls.Add(1)
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

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := calls.Load(); got == 0 {
		t.Fatalf("runner calls = %d, want > 0 in auto mode", got)
	}

	if err := c.Shutdown(); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	waitForDaemonExit(t, done, 2*time.Second)
}
