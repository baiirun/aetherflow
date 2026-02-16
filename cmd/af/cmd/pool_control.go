package cmd

import (
	"fmt"
	"os"

	"github.com/baiirun/aetherflow/internal/client"
	"github.com/baiirun/aetherflow/internal/term"
	"github.com/spf13/cobra"
)

var drainCmd = &cobra.Command{
	Use:   "drain",
	Short: "Stop scheduling new tasks, let current work finish",
	Long: `Transition the pool to draining mode.

No new tasks from the queue will be scheduled, but agents currently
running will continue until they finish. Crash respawns are still
allowed since those tasks are already claimed.

Use 'af resume' to return to normal scheduling.`,
	Run: func(cmd *cobra.Command, args []string) {
		c := client.New(resolveSocketPath(cmd))
		result, err := c.PoolDrain()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPoolModeResult(result)
	},
}

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Freeze the pool — no new scheduling or respawns",
	Long: `Transition the pool to paused mode.

No new tasks will be scheduled and crashed agents will not be
respawned. Agents currently running continue until they finish
or crash.

Use 'af resume' to return to normal scheduling.`,
	Run: func(cmd *cobra.Command, args []string) {
		c := client.New(resolveSocketPath(cmd))
		result, err := c.PoolPause()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPoolModeResult(result)
	},
}

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume normal pool scheduling",
	Long: `Transition the pool back to active mode from draining or paused.

Normal scheduling resumes: tasks from the queue will be assigned to
free slots and crashed agents will be respawned.`,
	Run: func(cmd *cobra.Command, args []string) {
		c := client.New(resolveSocketPath(cmd))
		result, err := c.PoolResume()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPoolModeResult(result)
	},
}

func printPoolModeResult(result *client.PoolModeResult) {
	var modeStr string
	switch result.Mode {
	case "active":
		modeStr = term.Green(result.Mode)
	case "draining":
		modeStr = term.Yellow(result.Mode)
	case "paused":
		modeStr = term.Red(result.Mode)
	default:
		modeStr = result.Mode
	}
	fmt.Printf("pool %s %s\n", modeStr, term.Dimf("(%d agents running)", result.Running))
}

func init() {
	rootCmd.AddCommand(drainCmd)
	rootCmd.AddCommand(pauseCmd)
	rootCmd.AddCommand(resumeCmd)

}
