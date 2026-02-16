package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/baiirun/aetherflow/internal/protocol"
	"gopkg.in/yaml.v3"
)

const (
	DefaultPoolSize          = 3
	DefaultSpawnCmd          = "opencode run --format json"
	DefaultMaxRetries        = 3
	DefaultLogDir            = ".aetherflow/logs"
	DefaultReconcileInterval = 30 * time.Second
)

// validProjectName restricts project names to safe characters for use in
// socket paths and log file paths. Rejects slashes, spaces, and other
// characters that could cause path traversal or shell interpretation issues.
var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validTaskID restricts task IDs parsed from prog output to safe characters
// before they flow into git commands (rev-parse, merge-base) and prog done.
// Without this, a crafted ID like "--exec=evil" could be interpreted as a flag.
var validTaskID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// Config holds daemon configuration.
//
// Configuration is assembled from three sources in priority order:
//  1. CLI flags (highest priority)
//  2. Config file (.aetherflow.yaml)
//  3. Defaults (lowest priority)
type Config struct {
	// SocketPath is the Unix socket path for the RPC server.
	SocketPath string `yaml:"socket_path"`

	// Project is the prog project to watch for tasks. Required.
	Project string `yaml:"project"`

	// PollInterval is how often to check prog for ready tasks.
	PollInterval time.Duration `yaml:"poll_interval"`

	// PoolSize is the maximum number of concurrent agent slots.
	PoolSize int `yaml:"pool_size"`

	// SpawnCmd is the command used to launch agent sessions.
	SpawnCmd string `yaml:"spawn_cmd"`

	// MaxRetries is the maximum number of crash respawns per task.
	MaxRetries int `yaml:"max_retries"`

	// PromptDir overrides the embedded prompt templates with files from this
	// directory. When empty, the daemon uses prompts compiled into the binary.
	// Set this for development or to customize agent behavior without rebuilding.
	PromptDir string `yaml:"prompt_dir"`

	// Solo mode has agents merge their branch directly to main instead of
	// creating a PR and waiting for review. Use when running a single agent
	// or when you want autonomous end-to-end delivery without a review gate.
	Solo bool `yaml:"solo"`

	// LogDir is the directory for agent JSONL log files.
	// Each task gets a <taskID>.jsonl file in this directory.
	LogDir string `yaml:"log_dir"`

	// ReconcileInterval is how often the daemon checks if reviewing tasks
	// have been merged to main. When a task's af/<task-id> branch is an
	// ancestor of main (or the branch no longer exists), the daemon
	// automatically marks the task done via `prog done`.
	ReconcileInterval time.Duration `yaml:"reconcile_interval"`

	// Runner is the command execution function. Not configurable via file/flags.
	Runner CommandRunner `yaml:"-"`

	// Starter is the process spawning function. Not configurable via file/flags.
	Starter ProcessStarter `yaml:"-"`

	// Logger is the structured logger. Not configurable via file/flags.
	Logger *slog.Logger `yaml:"-"`
}

// ApplyDefaults fills in zero-valued fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if c.SocketPath == "" {
		c.SocketPath = protocol.SocketPathFor(c.Project)
	}
	if c.PollInterval == 0 {
		c.PollInterval = DefaultPollInterval
	}
	if c.PoolSize == 0 {
		c.PoolSize = DefaultPoolSize
	}
	if c.SpawnCmd == "" {
		c.SpawnCmd = DefaultSpawnCmd
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = DefaultMaxRetries
	}
	// PromptDir intentionally has no default — empty means use embedded prompts.
	if c.ReconcileInterval == 0 {
		c.ReconcileInterval = DefaultReconcileInterval
	}
	if c.LogDir == "" {
		c.LogDir = DefaultLogDir
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Validate checks that configuration values are valid.
// Call after ApplyDefaults.
func (c *Config) Validate() error {
	if c.Project == "" {
		return fmt.Errorf("project is required (use --project or set project in config file)")
	}
	if !validProjectName.MatchString(c.Project) {
		return fmt.Errorf("project name %q contains invalid characters (allowed: letters, digits, hyphens, underscores, dots)", c.Project)
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("poll-interval must be positive, got %v", c.PollInterval)
	}
	if c.PoolSize <= 0 {
		return fmt.Errorf("pool-size must be positive, got %d", c.PoolSize)
	}
	if c.SpawnCmd == "" {
		return fmt.Errorf("spawn-cmd must not be empty")
	}
	if c.MaxRetries < 0 {
		return fmt.Errorf("max-retries must be non-negative, got %d", c.MaxRetries)
	}
	if c.ReconcileInterval < 5*time.Second {
		return fmt.Errorf("reconcile-interval must be at least 5s, got %v", c.ReconcileInterval)
	}

	// When PromptDir is set (filesystem override), resolve to absolute path
	// and verify the directory contains the required prompt files.
	// When empty, embedded prompts are used and no filesystem check is needed.
	if c.PromptDir != "" {
		if !filepath.IsAbs(c.PromptDir) {
			abs, err := filepath.Abs(c.PromptDir)
			if err != nil {
				return fmt.Errorf("resolving prompt-dir %q: %w", c.PromptDir, err)
			}
			c.PromptDir = abs
		}
		// Only check worker.md for now — InferRole always returns RoleWorker.
		// When planner role is enabled, add planner.md check here too.
		if _, err := os.Stat(filepath.Join(c.PromptDir, "worker.md")); err != nil {
			return fmt.Errorf("prompt-dir %q must contain worker.md: %w", c.PromptDir, err)
		}
		if c.Logger != nil {
			c.Logger.Info("using filesystem prompts", "prompt_dir", c.PromptDir)
		}
	} else if c.Logger != nil {
		c.Logger.Info("using embedded prompts")
	}

	// Resolve LogDir to absolute path so detached daemons don't depend on cwd.
	if !filepath.IsAbs(c.LogDir) {
		abs, err := filepath.Abs(c.LogDir)
		if err != nil {
			return fmt.Errorf("resolving log-dir %q: %w", c.LogDir, err)
		}
		c.LogDir = abs
	}

	return nil
}

// LoadConfigFile reads a YAML config file and merges it into the config.
// Only zero-valued fields are overwritten — CLI flags take precedence.
// Returns nil if the file does not exist.
func LoadConfigFile(path string, into *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading config file %s: %w", path, err)
	}

	var file Config
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parsing config file %s: %w", path, err)
	}

	mergeConfig(&file, into)
	return nil
}

// mergeConfig copies non-zero fields from src into dst, but only where
// dst has the zero value. This means CLI flags (set on dst before merge)
// take priority over file values.
func mergeConfig(src, dst *Config) {
	if dst.SocketPath == "" {
		dst.SocketPath = src.SocketPath
	}
	if dst.Project == "" {
		dst.Project = src.Project
	}
	if dst.PollInterval == 0 {
		dst.PollInterval = src.PollInterval
	}
	if dst.PoolSize == 0 {
		dst.PoolSize = src.PoolSize
	}
	if dst.SpawnCmd == "" {
		dst.SpawnCmd = src.SpawnCmd
	}
	if dst.MaxRetries == 0 {
		dst.MaxRetries = src.MaxRetries
	}
	if dst.PromptDir == "" {
		dst.PromptDir = src.PromptDir
	}
	if dst.ReconcileInterval == 0 {
		dst.ReconcileInterval = src.ReconcileInterval
	}
	// Solo is a bool — only override if dst hasn't been set by CLI flag.
	// Since bool zero is false, we can only merge true from file.
	if src.Solo && !dst.Solo {
		dst.Solo = true
	}
	if dst.LogDir == "" {
		dst.LogDir = src.LogDir
	}
}
