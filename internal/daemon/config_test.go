package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testConfigPromptDir creates a temp directory with worker.md for config validation tests.
func testConfigPromptDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "worker.md"), []byte("# Worker\nTask: {{task_id}}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConfigApplyDefaults(t *testing.T) {
	var cfg Config
	cfg.ApplyDefaults()

	if cfg.SocketPath != DefaultSocketPath {
		t.Errorf("SocketPath = %q, want %q", cfg.SocketPath, DefaultSocketPath)
	}
	if cfg.PollInterval != DefaultPollInterval {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, DefaultPollInterval)
	}
	if cfg.PoolSize != DefaultPoolSize {
		t.Errorf("PoolSize = %d, want %d", cfg.PoolSize, DefaultPoolSize)
	}
	if cfg.SpawnCmd != DefaultSpawnCmd {
		t.Errorf("SpawnCmd = %q, want %q", cfg.SpawnCmd, DefaultSpawnCmd)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, DefaultMaxRetries)
	}
	if cfg.PromptDir != DefaultPromptDir {
		t.Errorf("PromptDir = %q, want %q", cfg.PromptDir, DefaultPromptDir)
	}
	if cfg.Logger == nil {
		t.Error("Logger should not be nil after ApplyDefaults")
	}
}

func TestConfigApplyDefaultsPreservesExisting(t *testing.T) {
	cfg := Config{
		SocketPath:   "/custom/sock",
		PollInterval: 30 * time.Second,
		PoolSize:     5,
		SpawnCmd:     "custom-cmd",
		MaxRetries:   10,
		PromptDir:    "/custom/prompts",
	}
	cfg.ApplyDefaults()

	if cfg.SocketPath != "/custom/sock" {
		t.Errorf("SocketPath = %q, want %q", cfg.SocketPath, "/custom/sock")
	}
	if cfg.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 30*time.Second)
	}
	if cfg.PoolSize != 5 {
		t.Errorf("PoolSize = %d, want %d", cfg.PoolSize, 5)
	}
	if cfg.SpawnCmd != "custom-cmd" {
		t.Errorf("SpawnCmd = %q, want %q", cfg.SpawnCmd, "custom-cmd")
	}
	if cfg.MaxRetries != 10 {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, 10)
	}
	if cfg.PromptDir != "/custom/prompts" {
		t.Errorf("PromptDir = %q, want %q", cfg.PromptDir, "/custom/prompts")
	}
}

func TestConfigValidate(t *testing.T) {
	promptDir := testConfigPromptDir(t)

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "missing project",
			cfg:     Config{PollInterval: time.Second, PoolSize: 1, SpawnCmd: "cmd"},
			wantErr: "project is required",
		},
		{
			name:    "negative poll interval",
			cfg:     Config{Project: "test", PollInterval: -1, PoolSize: 1, SpawnCmd: "cmd"},
			wantErr: "poll-interval must be positive",
		},
		{
			name:    "zero pool size",
			cfg:     Config{Project: "test", PollInterval: time.Second, PoolSize: 0, SpawnCmd: "cmd"},
			wantErr: "pool-size must be positive",
		},
		{
			name:    "negative pool size",
			cfg:     Config{Project: "test", PollInterval: time.Second, PoolSize: -1, SpawnCmd: "cmd"},
			wantErr: "pool-size must be positive",
		},
		{
			name:    "empty spawn cmd",
			cfg:     Config{Project: "test", PollInterval: time.Second, PoolSize: 1, SpawnCmd: ""},
			wantErr: "spawn-cmd must not be empty",
		},
		{
			name:    "negative max retries",
			cfg:     Config{Project: "test", PollInterval: time.Second, PoolSize: 1, SpawnCmd: "cmd", MaxRetries: -1},
			wantErr: "max-retries must be non-negative",
		},
		{
			name:    "missing prompt dir",
			cfg:     Config{Project: "test", PollInterval: time.Second, PoolSize: 1, SpawnCmd: "cmd", PromptDir: "/nonexistent/prompts"},
			wantErr: "prompt-dir",
		},
		{
			name: "valid config",
			cfg: Config{
				Project:      "test",
				PollInterval: 10 * time.Second,
				PoolSize:     3,
				SpawnCmd:     "opencode run",
				MaxRetries:   3,
				PromptDir:    promptDir,
			},
			wantErr: "",
		},
		{
			name: "zero max retries is valid",
			cfg: Config{
				Project:      "test",
				PollInterval: time.Second,
				PoolSize:     1,
				SpawnCmd:     "cmd",
				MaxRetries:   0,
				PromptDir:    promptDir,
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestConfigValidateAfterDefaults(t *testing.T) {
	// A config with just Project and PromptDir set should be valid after defaults.
	cfg := Config{
		Project:   "myproject",
		PromptDir: testConfigPromptDir(t),
	}
	cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config after defaults, got: %v", err)
	}
}

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".aetherflow.yaml")

	yaml := `project: from-file
socket_path: /tmp/custom.sock
poll_interval: 30s
pool_size: 5
spawn_cmd: custom-agent
max_retries: 7
prompt_dir: /custom/prompts
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := LoadConfigFile(path, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Project != "from-file" {
		t.Errorf("Project = %q, want %q", cfg.Project, "from-file")
	}
	if cfg.SocketPath != "/tmp/custom.sock" {
		t.Errorf("SocketPath = %q, want %q", cfg.SocketPath, "/tmp/custom.sock")
	}
	if cfg.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 30*time.Second)
	}
	if cfg.PoolSize != 5 {
		t.Errorf("PoolSize = %d, want %d", cfg.PoolSize, 5)
	}
	if cfg.SpawnCmd != "custom-agent" {
		t.Errorf("SpawnCmd = %q, want %q", cfg.SpawnCmd, "custom-agent")
	}
	if cfg.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, 7)
	}
	if cfg.PromptDir != "/custom/prompts" {
		t.Errorf("PromptDir = %q, want %q", cfg.PromptDir, "/custom/prompts")
	}
}

func TestLoadConfigFileFlagOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".aetherflow.yaml")

	yaml := `project: from-file
pool_size: 10
spawn_cmd: file-cmd
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate CLI flags by pre-setting some values.
	cfg := Config{
		Project:  "from-flag",
		PoolSize: 7,
	}

	if err := LoadConfigFile(path, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CLI values should be preserved.
	if cfg.Project != "from-flag" {
		t.Errorf("Project = %q, want %q (flag should override file)", cfg.Project, "from-flag")
	}
	if cfg.PoolSize != 7 {
		t.Errorf("PoolSize = %d, want %d (flag should override file)", cfg.PoolSize, 7)
	}

	// File values should fill gaps.
	if cfg.SpawnCmd != "file-cmd" {
		t.Errorf("SpawnCmd = %q, want %q (should come from file)", cfg.SpawnCmd, "file-cmd")
	}
}

func TestLoadConfigFileMissing(t *testing.T) {
	var cfg Config
	if err := LoadConfigFile("/nonexistent/.aetherflow.yaml", &cfg); err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
}

func TestLoadConfigFileInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".aetherflow.yaml")

	if err := os.WriteFile(path, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := LoadConfigFile(path, &cfg); err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadConfigFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".aetherflow.yaml")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Project: "preserved"}
	if err := LoadConfigFile(path, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Project != "preserved" {
		t.Errorf("Project = %q, want %q (empty file should not clear values)", cfg.Project, "preserved")
	}
}
