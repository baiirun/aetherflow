package daemon

import (
	"context"
	"encoding/json"
	"testing"
)

func TestHandlePoolDrainHappyPath(t *testing.T) {
	cfg := Config{
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: testPromptDir(t),
		LogDir:    t.TempDir(),
	}
	cfg.ApplyDefaults()

	pool := NewPool(cfg, nil, nil, testLogger())
	pool.ctx = context.Background()

	d := &Daemon{config: cfg, pool: pool, log: testLogger()}

	resp := d.handlePoolDrain()
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result PoolModeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Mode != PoolDraining {
		t.Errorf("mode = %q, want %q", result.Mode, PoolDraining)
	}
	if pool.Mode() != PoolDraining {
		t.Errorf("pool.Mode() = %q, want %q", pool.Mode(), PoolDraining)
	}
}

func TestHandlePoolPauseHappyPath(t *testing.T) {
	cfg := Config{
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: testPromptDir(t),
		LogDir:    t.TempDir(),
	}
	cfg.ApplyDefaults()

	pool := NewPool(cfg, nil, nil, testLogger())
	pool.ctx = context.Background()

	d := &Daemon{config: cfg, pool: pool, log: testLogger()}

	resp := d.handlePoolPause()
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result PoolModeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Mode != PoolPaused {
		t.Errorf("mode = %q, want %q", result.Mode, PoolPaused)
	}
}

func TestHandlePoolResumeHappyPath(t *testing.T) {
	cfg := Config{
		Project:   "testproject",
		PoolSize:  2,
		SpawnCmd:  "fake-agent",
		PromptDir: testPromptDir(t),
		LogDir:    t.TempDir(),
	}
	cfg.ApplyDefaults()

	pool := NewPool(cfg, nil, nil, testLogger())
	pool.ctx = context.Background()

	// Start from paused.
	pool.Pause()

	d := &Daemon{config: cfg, pool: pool, log: testLogger()}

	resp := d.handlePoolResume()
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result PoolModeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Mode != PoolActive {
		t.Errorf("mode = %q, want %q", result.Mode, PoolActive)
	}
}

func TestHandlePoolControlNilPool(t *testing.T) {
	d := &Daemon{config: Config{}, pool: nil, log: testLogger()}

	for _, handler := range []func() *Response{
		d.handlePoolDrain,
		d.handlePoolPause,
		d.handlePoolResume,
	} {
		resp := handler()
		if resp.Success {
			t.Error("expected error for nil pool")
		}
		if resp.Error != "no pool configured" {
			t.Errorf("error = %q, want %q", resp.Error, "no pool configured")
		}
	}
}
