package cmd

import (
	"fmt"
	"os"

	"github.com/baiirun/aetherflow/internal/daemon"
	"github.com/baiirun/aetherflow/internal/protocol"
	"github.com/baiirun/aetherflow/internal/term"
	"github.com/spf13/cobra"
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

// SetVersion sets the version string shown by --version.
// Called from main with the value injected by goreleaser ldflags.
func SetVersion(v string) {
	rootCmd.Version = v
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("config", "c", "", "config file (default is $HOME/.aetherflow.yaml)")
	rootCmd.PersistentFlags().StringP("project", "p", "", "Project name (targets a project-scoped daemon URL when set, overrides config file)")
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

// resolveDaemonURL determines the daemon URL from the CLI flags,
// config file, and daemon mode. Priority:
//  1. Explicit --project -> project-scoped daemon URL
//  2. Explicit/configured listen_addr -> canonical daemon URL
//  3. Auto mode + configured project -> project-scoped daemon URL
//  4. Manual mode default -> DefaultDaemonURL
func resolveDaemonURL(cmd *cobra.Command) string {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = ".aetherflow.yaml"
	}
	var cfg daemon.Config
	if err := daemon.LoadConfigFile(configPath, &cfg); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "warning: failed to parse %s: %v (using default daemon URL)\n", configPath, err)
	}

	explicitProject := ""
	if cmd.Flags().Changed("project") {
		explicitProject, _ = cmd.Flags().GetString("project")
		cfg.Project = explicitProject
	}
	if cmd.Flags().Lookup("spawn-policy") != nil && cmd.Flags().Changed("spawn-policy") {
		policy, _ := cmd.Flags().GetString("spawn-policy")
		cfg.SpawnPolicy = daemon.SpawnPolicy(policy)
	}

	normalizedPolicy := cfg.SpawnPolicy.Normalized()
	if explicitProject != "" {
		return protocol.DaemonURLFor(explicitProject)
	}

	if listenAddr := cfg.ListenAddr; listenAddr != "" {
		daemonURL, err := protocol.DaemonURLFromListenAddr(cfg.ListenAddr)
		if err == nil {
			return daemonURL
		}
		fmt.Fprintf(os.Stderr, "warning: invalid listen_addr %q in %s: %v (using default daemon URL)\n", listenAddr, configPath, err)
	}
	if normalizedPolicy == daemon.SpawnPolicyAuto && cfg.Project != "" {
		return protocol.DaemonURLFor(cfg.Project)
	}

	return protocol.DefaultDaemonURL
}

// Fatal prints an error and exits.
func Fatal(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+msg+"\n", args...)
	os.Exit(1)
}
