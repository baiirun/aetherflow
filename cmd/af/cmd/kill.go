package cmd

import (
	"fmt"
	"os"

	"github.com/geobrowser/aetherflow/internal/client"
	"github.com/geobrowser/aetherflow/internal/term"
	"github.com/spf13/cobra"
)

var killCmd = &cobra.Command{
	Use:   "kill <agent-name>",
	Short: "Terminate a running agent",
	Long: `Send SIGTERM to a running agent.

The agent process is terminated and the pool slot is freed.
The existing reap() logic handles cleanup and decides whether
to respawn based on pool mode and retry limits.

Use 'af status' to see running agents and their names.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		agentName := args[0]

		c := client.New(resolveSocketPath(cmd))
		result, err := c.KillAgent(agentName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("%s agent %s (PID %d)\n",
			term.Green("killed"),
			term.Bold(result.AgentName),
			result.PID,
		)
	},
}

func init() {
	rootCmd.AddCommand(killCmd)
}
