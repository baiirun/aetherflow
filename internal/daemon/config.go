package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPoolSize   = 3
	DefaultSpawnCmd   = "opencode run"
	DefaultMaxRetries = 3
)

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
		c.SocketPath = DefaultSocketPath
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
}
