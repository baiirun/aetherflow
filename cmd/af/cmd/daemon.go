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
	Long: `Start the aetherflow daemon.

By default runs in foreground. Use -d to run in background.`,
	Run: func(cmd *cobra.Command, args []string) {
		background, _ := cmd.Flags().GetBool("detach")

		if background {
			// Re-exec ourselves without -d flag
			exe, err := os.Executable()
			if err != nil {
				Fatal("failed to get executable: %v", err)
			}

			proc := exec.Command(exe, "daemon", "start")
			proc.SysProcAttr = &syscall.SysProcAttr{
				Setsid: true, // Create new session, detach from terminal
			}

			if err := proc.Start(); err != nil {
				Fatal("failed to start daemon: %v", err)
			}

			fmt.Printf("daemon started (pid %d)\n", proc.Process.Pid)
			return
		}

		// Foreground mode
		d := daemon.New("")
		if err := d.Run(); err != nil {
			Fatal("%v", err)
		}
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the daemon",
	Run: func(cmd *cobra.Command, args []string) {
		c := client.New("")
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

	daemonStartCmd.Flags().BoolP("detach", "d", false, "Run in background")
}
