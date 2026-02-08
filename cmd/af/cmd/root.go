package cmd

import (
	"fmt"
	"os"

	"github.com/geobrowser/aetherflow/internal/protocol"
	"github.com/geobrowser/aetherflow/internal/term"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var rootCmd = &cobra.Command{
	Use:   "af",
	Short: "Aetherflow CLI - async runtime for agent work scheduling",
	Long: `af is the CLI for aetherflow, an async runtime for agent work scheduling.

It turns intent into reliable, high-quality work across non-deterministic
agents by combining a central task system with lightweight messaging and
clear state transitions.

The daemon (aetherd) must be running for most commands to work.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("config", "c", "", "config file (default is $HOME/.aetherflow.yaml)")
	rootCmd.PersistentFlags().StringP("project", "p", "", "Project name (derives socket path, overrides config file)")
	rootCmd.PersistentFlags().String("socket", "", "Unix socket path (overrides --project and config)")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable colored output")

	// Wire --no-color to the term package. OnInitialize runs before any
	// PreRun hooks and doesn't participate in Cobra's override chain, so
	// subcommands can freely set their own PersistentPreRun without breaking this.
	cobra.OnInitialize(func() {
		if noColor, _ := rootCmd.Flags().GetBool("no-color"); noColor {
			term.Disable(true)
		}
	})
}

// resolveSocketPath determines the daemon socket path from the CLI flag,
// config file, or default convention. Priority:
//  1. Explicit --socket flag (full path)
//  2. Explicit --project flag → project-scoped socket path
//  3. Project from config file → project-scoped socket path
//  4. DefaultSocketPath fallback
func resolveSocketPath(cmd *cobra.Command) string {
	if cmd.Flags().Changed("socket") {
		s, _ := cmd.Flags().GetString("socket")
		return s
	}

	if cmd.Flags().Changed("project") {
		p, _ := cmd.Flags().GetString("project")
		return protocol.SocketPathFor(p)
	}

	// Discover the project from the config file to derive the socket path.
	// Only the project field is needed — we parse a minimal struct to avoid
	// importing the daemon package into the CLI layer.
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = ".aetherflow.yaml"
	}
	data, err := os.ReadFile(configPath)
	if err == nil {
		var partial struct {
			Project string `yaml:"project"`
		}
		if err := yaml.Unmarshal(data, &partial); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to parse %s: %v (using default socket)\n", configPath, err)
		} else if partial.Project != "" {
			return protocol.SocketPathFor(partial.Project)
		}
	}
	// File doesn't exist or has no project — use the global default.

	return protocol.DefaultSocketPath
}

// Fatal prints an error and exits.
func Fatal(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+msg+"\n", args...)
	os.Exit(1)
}
