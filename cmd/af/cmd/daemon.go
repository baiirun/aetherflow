package cmd

import (
	"fmt"

	"github.com/geobrowser/aetherflow/internal/client"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Check daemon status",
	Long: `Check if aetherd is running and display its status.

To start the daemon, run 'aetherd' directly.`,
	Run: func(cmd *cobra.Command, args []string) {
		c := client.New("")
		status, err := c.Status()
		if err != nil {
			fmt.Println("aetherd: not running")
			fmt.Println("\nTo start: aetherd")
			return
		}

		fmt.Println("aetherd: running")
		if socket, ok := status["socket"]; ok {
			fmt.Printf("  socket: %v\n", socket)
		}
		if count, ok := status["agent_count"]; ok {
			fmt.Printf("  agents: %v\n", count)
		}
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}
