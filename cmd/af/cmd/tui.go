package cmd

import (
	"fmt"
	"os"

	"github.com/baiirun/aetherflow/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch interactive daemon dashboard",
	Long: `Launch a full-screen terminal dashboard for monitoring the aetherflow daemon.

The TUI provides a k9s/btop-style interface with:
  - Dashboard: pool overview, agent table, task queue
  - Agent detail: task info, prog logs, tool call stream
  - Log stream: full-screen event log tail

Navigation:
  j/k    Navigate / scroll
  Enter  Drill into agent detail
  l      Open log stream
  Tab    Cycle panes
  p      Pause/resume pool
  d      Drain pool
  ?      Help
  q      Back / quit

Requires a running daemon.`,
	Run: func(cmd *cobra.Command, args []string) {
		socketPath := resolveSocketPath(cmd)

		cfg := tui.Config{
			SocketPath: socketPath,
		}

		if err := tui.Run(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
