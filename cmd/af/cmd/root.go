package cmd

import (
	"fmt"
	"os"

	"github.com/geobrowser/aetherflow/internal/term"
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

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("config", "c", "", "config file (default is $HOME/.aetherflow.yaml)")
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

// Fatal prints an error and exits.
func Fatal(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+msg+"\n", args...)
	os.Exit(1)
}
