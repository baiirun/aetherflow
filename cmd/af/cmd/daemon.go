package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/geobrowser/aetherflow/internal/client"
	"github.com/geobrowser/aetherflow/internal/daemon"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the daemon",
	Long:  `Start the daemon or check its status.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Default: show status
		c := client.New(resolveSocketPath(cmd))
		status, err := c.StatusFull()
		if err != nil {
			fmt.Println("not running")
			fmt.Println("\nTo start: af daemon start --project <name>")
			return
		}

		fmt.Printf("running (pool: %d, project: %s)\n", status.PoolSize, status.Project)
	},
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon",
	Long: `Start the aetherflow daemon.

By default runs in foreground. Use -d to run in background.

Configuration is loaded from (in priority order):
  1. CLI flags (highest)
  2. Config file (.aetherflow.yaml in current directory)
  3. Defaults (lowest)`,
	Run: func(cmd *cobra.Command, args []string) {
		background, _ := cmd.Flags().GetBool("detach")

		if background {
			startDetached(cmd)
			return
		}

		cfg := buildConfig(cmd)
		d := daemon.New(cfg)
		if err := d.Run(); err != nil {
			Fatal("%v", err)
		}
	},
}

// buildConfig assembles a Config from CLI flags and config file.
func buildConfig(cmd *cobra.Command) daemon.Config {
	var cfg daemon.Config

	// Read CLI flags into config. Only non-default values override.
	if cmd.Flags().Changed("project") {
		cfg.Project, _ = cmd.Flags().GetString("project")
	}
	if cmd.Flags().Changed("socket") {
		cfg.SocketPath, _ = cmd.Flags().GetString("socket")
	}
	if cmd.Flags().Changed("poll-interval") {
		cfg.PollInterval, _ = cmd.Flags().GetDuration("poll-interval")
	}
	if cmd.Flags().Changed("pool-size") {
		cfg.PoolSize, _ = cmd.Flags().GetInt("pool-size")
	}
	if cmd.Flags().Changed("spawn-cmd") {
		cfg.SpawnCmd, _ = cmd.Flags().GetString("spawn-cmd")
	}
	if cmd.Flags().Changed("max-retries") {
		cfg.MaxRetries, _ = cmd.Flags().GetInt("max-retries")
	}

	// Load config file (only fills zero-valued fields).
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = ".aetherflow.yaml"
	}
	if err := daemon.LoadConfigFile(configPath, &cfg); err != nil {
		Fatal("%v", err)
	}

	// Apply defaults for anything still unset, then validate.
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		Fatal("%v", err)
	}

	return cfg
}

// startDetached re-execs the daemon in the background.
func startDetached(cmd *cobra.Command) {
	exe, err := os.Executable()
	if err != nil {
		Fatal("failed to get executable: %v", err)
	}

	// Forward all flags except --detach.
	reArgs := []string{"daemon", "start"}
	for _, name := range []string{"project", "socket", "poll-interval", "pool-size", "spawn-cmd", "max-retries", "config"} {
		if cmd.Flags().Changed(name) {
			val, _ := cmd.Flags().GetString(name)
			// Duration and int flags also work with GetString via pflag.
			if val == "" {
				// For non-string flags, get the string representation.
				val = cmd.Flags().Lookup(name).Value.String()
			}
			reArgs = append(reArgs, "--"+name, val)
		}
	}

	proc := exec.Command(exe, reArgs...)
	proc.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session, detach from terminal
	}

	if err := proc.Start(); err != nil {
		Fatal("failed to start daemon: %v", err)
	}

	fmt.Printf("daemon started (pid %d)\n", proc.Process.Pid)
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the daemon",
	Run: func(cmd *cobra.Command, args []string) {
		c := client.New(resolveSocketPath(cmd))
		if err := c.Shutdown(); err != nil {
			Fatal("%v", err)
		}
		fmt.Println("daemon stopped")
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)

	f := daemonStartCmd.Flags()
	f.BoolP("detach", "d", false, "Run in background")
	f.StringP("project", "p", "", "Project to watch for tasks (required)")
	f.Duration("poll-interval", daemon.DefaultPollInterval, "How often to poll prog for tasks")
	f.Int("pool-size", daemon.DefaultPoolSize, "Maximum concurrent agent slots")
	f.String("spawn-cmd", daemon.DefaultSpawnCmd, "Command to launch agent sessions")
	f.Int("max-retries", daemon.DefaultMaxRetries, "Max crash respawns per task")
	f.String("config", "", "Config file path (default: .aetherflow.yaml)")
}
