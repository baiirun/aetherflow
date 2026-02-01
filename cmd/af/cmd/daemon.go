package cmd

import (
	"fmt"

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
		c := client.New("")
		status, err := c.Status()
		if err != nil {
			fmt.Println("not running")
			fmt.Println("\nTo start: af daemon start")
			return
		}

		fmt.Println("running")
		if agents, ok := status["agents"]; ok {
			fmt.Printf("  agents: %v\n", agents)
		}
	},
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon",
	Long:  `Start the aetherflow daemon. Runs in foreground by default.`,
	Run: func(cmd *cobra.Command, args []string) {
		d := daemon.New("")
		if err := d.Run(); err != nil {
			Fatal("%v", err)
		}
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the daemon",
	Long:  `Stop the running daemon.`,
	Run: func(cmd *cobra.Command, args []string) {
		// TODO: Send shutdown signal to daemon
		fmt.Println("Send SIGTERM to the daemon process to stop it")
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
}
