//go:build integration

package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/baiirun/aetherflow/internal/client"
	"github.com/baiirun/aetherflow/internal/protocol"
)

// startTestDaemon starts a daemon on an ephemeral port and returns a client
// pointed at it. The daemon is shut down automatically when the test ends.
func startTestDaemon(t *testing.T) *client.Client {
	t.Helper()
	listenAddr := testListenAddr(t)
	cfg := Config{
		ListenAddr:        listenAddr,
		PollInterval:      10 * time.Millisecond,
		PoolSize:          3,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual,
		ReconcileInterval: DefaultReconcileInterval,
		ServerStarter:     noopServerStarter,
		SessionDir:        t.TempDir(),
	}
	d := New(cfg)
	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	c := client.New(fmt.Sprintf("http://%s", listenAddr))
	waitForDaemonStatus(t, c, 2*time.Second)

	t.Cleanup(func() {
		if err := c.Shutdown(); err != nil {
			t.Logf("shutdown: %v", err)
		}
		waitForDaemonExit(t, done, 2*time.Second)
	})

	return c
}

// TestHTTPStatusReturnsInitialState verifies the /api/v1/status response shape
// for a freshly started daemon with no agents or tasks.
func TestHTTPStatusReturnsInitialState(t *testing.T) {
	c := startTestDaemon(t)

	status, err := c.StatusFull()
	if err != nil {
		t.Fatalf("StatusFull: %v", err)
	}

	if status.PoolSize != 3 {
		t.Errorf("PoolSize = %d, want 3", status.PoolSize)
	}
	if status.SpawnPolicy != string(SpawnPolicyManual) {
		t.Errorf("SpawnPolicy = %q, want %q", status.SpawnPolicy, SpawnPolicyManual)
	}
	if len(status.Agents) != 0 {
		t.Errorf("Agents = %d, want 0", len(status.Agents))
	}
	if len(status.Spawns) != 0 {
		t.Errorf("Spawns = %d, want 0", len(status.Spawns))
	}
}

// TestHTTPLifecycleReturnsRunning verifies the /api/v1/lifecycle endpoint
// returns the running state once the daemon is up.
func TestHTTPLifecycleReturnsRunning(t *testing.T) {
	c := startTestDaemon(t)

	lc, err := c.DaemonLifecycle()
	if err != nil {
		t.Fatalf("DaemonLifecycle: %v", err)
	}

	if lc.State != protocol.LifecycleStateRunning {
		t.Errorf("State = %q, want %q", lc.State, protocol.LifecycleStateRunning)
	}
	if lc.SpawnPolicy != string(SpawnPolicyManual) {
		t.Errorf("SpawnPolicy = %q, want %q", lc.SpawnPolicy, SpawnPolicyManual)
	}
}

// TestHTTPSpawnRegistrationLifecycle exercises the full spawn register →
// visible in status → deregister → marked exited flow over HTTP.
func TestHTTPSpawnRegistrationLifecycle(t *testing.T) {
	c := startTestDaemon(t)

	spawnID := "spawn-integration-test-abc1"
	prompt := "integration test spawn"

	// Register a spawn.
	if err := c.SpawnRegister(client.SpawnRegisterParams{
		SpawnID: spawnID,
		PID:     99999,
		Prompt:  prompt,
	}); err != nil {
		t.Fatalf("SpawnRegister: %v", err)
	}

	// It should appear in status as running.
	status, err := c.StatusFull()
	if err != nil {
		t.Fatalf("StatusFull after register: %v", err)
	}
	if len(status.Spawns) != 1 {
		t.Fatalf("Spawns = %d, want 1", len(status.Spawns))
	}
	s := status.Spawns[0]
	if s.SpawnID != spawnID {
		t.Errorf("SpawnID = %q, want %q", s.SpawnID, spawnID)
	}
	if s.Prompt != prompt {
		t.Errorf("Prompt = %q, want %q", s.Prompt, prompt)
	}
	if s.State != string(SpawnRunning) {
		t.Errorf("State = %q, want %q", s.State, SpawnRunning)
	}

	// Deregister marks it as exited.
	if err := c.SpawnDeregister(spawnID); err != nil {
		t.Fatalf("SpawnDeregister: %v", err)
	}

	status, err = c.StatusFull()
	if err != nil {
		t.Fatalf("StatusFull after deregister: %v", err)
	}
	if len(status.Spawns) != 1 {
		t.Fatalf("Spawns = %d, want 1 (exited entry)", len(status.Spawns))
	}
	if status.Spawns[0].State != string(SpawnExited) {
		t.Errorf("State after deregister = %q, want %q", status.Spawns[0].State, SpawnExited)
	}
}

// TestHTTPSpawnMultipleVisible registers several spawns and confirms they all
// appear in status.
func TestHTTPSpawnMultipleVisible(t *testing.T) {
	c := startTestDaemon(t)

	spawns := []client.SpawnRegisterParams{
		{SpawnID: "spawn-a", PID: 10001, Prompt: "task A"},
		{SpawnID: "spawn-b", PID: 10002, Prompt: "task B"},
		{SpawnID: "spawn-c", PID: 10003, Prompt: "task C"},
	}
	for _, p := range spawns {
		if err := c.SpawnRegister(p); err != nil {
			t.Fatalf("SpawnRegister(%s): %v", p.SpawnID, err)
		}
	}

	status, err := c.StatusFull()
	if err != nil {
		t.Fatalf("StatusFull: %v", err)
	}
	if len(status.Spawns) != 3 {
		t.Fatalf("Spawns = %d, want 3", len(status.Spawns))
	}

	byID := make(map[string]client.SpawnStatus, len(status.Spawns))
	for _, s := range status.Spawns {
		byID[s.SpawnID] = s
	}
	for _, want := range spawns {
		got, ok := byID[want.SpawnID]
		if !ok {
			t.Errorf("spawn %q missing from status", want.SpawnID)
			continue
		}
		if got.Prompt != want.Prompt {
			t.Errorf("spawn %q prompt = %q, want %q", want.SpawnID, got.Prompt, want.Prompt)
		}
		if got.State != string(SpawnRunning) {
			t.Errorf("spawn %q state = %q, want running", want.SpawnID, got.State)
		}
	}
}

// TestHTTPPoolControlTransitions exercises drain → pause → resume over HTTP
// and verifies the pool mode field changes accordingly.
// Requires a project-configured daemon so the pool is initialized.
func TestHTTPPoolControlTransitions(t *testing.T) {
	listenAddr := testListenAddr(t)
	cfg := Config{
		ListenAddr:        listenAddr,
		Project:           "integration-test",
		PollInterval:      10 * time.Millisecond,
		PoolSize:          3,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual, // manual: pool exists but auto-scheduling disabled
		ReconcileInterval: DefaultReconcileInterval,
		Runner:            func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil },
		ServerStarter:     noopServerStarter,
		SessionDir:        t.TempDir(),
	}
	d := New(cfg)
	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	c := client.New(fmt.Sprintf("http://%s", listenAddr))
	waitForDaemonStatus(t, c, 2*time.Second)
	t.Cleanup(func() {
		_ = c.Shutdown()
		waitForDaemonExit(t, done, 2*time.Second)
	})

	// Drain.
	res, err := c.PoolDrain()
	if err != nil {
		t.Fatalf("PoolDrain: %v", err)
	}
	if res.Mode != "draining" {
		t.Errorf("PoolDrain mode = %q, want draining", res.Mode)
	}

	// Pause.
	res, err = c.PoolPause()
	if err != nil {
		t.Fatalf("PoolPause: %v", err)
	}
	if res.Mode != "paused" {
		t.Errorf("PoolPause mode = %q, want paused", res.Mode)
	}

	// Resume.
	res, err = c.PoolResume()
	if err != nil {
		t.Fatalf("PoolResume: %v", err)
	}
	if res.Mode != "active" {
		t.Errorf("PoolResume mode = %q, want active", res.Mode)
	}

	// Status reflects active mode.
	status, err := c.StatusFull()
	if err != nil {
		t.Fatalf("StatusFull: %v", err)
	}
	if status.PoolMode != "" && status.PoolMode != "active" {
		t.Errorf("PoolMode = %q, want active or empty", status.PoolMode)
	}
}

// TestHTTPShutdownIsClean confirms the daemon exits with no error when
// shutdown is requested over HTTP.
func TestHTTPShutdownIsClean(t *testing.T) {
	listenAddr := testListenAddr(t)
	cfg := Config{
		ListenAddr:        listenAddr,
		PollInterval:      10 * time.Millisecond,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual,
		ReconcileInterval: DefaultReconcileInterval,
		ServerStarter:     noopServerStarter,
		SessionDir:        t.TempDir(),
	}
	d := New(cfg)
	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	c := client.New(fmt.Sprintf("http://%s", listenAddr))
	waitForDaemonStatus(t, c, 2*time.Second)

	if err := c.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	waitForDaemonExit(t, done, 2*time.Second)

	// Port should be released.
	c2 := client.New(fmt.Sprintf("http://%s", listenAddr))
	if _, err := c2.StatusFull(); err == nil {
		t.Error("daemon still responding after shutdown")
	}
}
