package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/baiirun/aetherflow/internal/client"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <agent-name>",
	Short: "Stream an agent's event log",
	Long: `Show events for a running agent.

Events are read from the daemon's in-memory event buffer (populated by the
opencode plugin event pipeline). By default shows all events formatted as
human-readable output. Use --raw to see raw JSON, -n to limit the initial
count, and -f/--follow or -w/--watch to stream new events as they arrive.

Requires a running daemon and an active agent.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		follow, _ := cmd.Flags().GetBool("follow")
		watch, _ := cmd.Flags().GetBool("watch")
		lines, _ := cmd.Flags().GetInt("lines")
		raw, _ := cmd.Flags().GetBool("raw")

		// Both --follow and --watch enable streaming; treat them as aliases.
		streaming := follow || watch
		_ = raw // reserved for future --raw flag (events.list raw=true)

		c := client.New(resolveSocketPath(cmd))
		result, err := c.EventsList(args[0], 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		// Print last n lines from the initial fetch.
		eventLines := result.Lines
		start := 0
		if lines > 0 && lines < len(eventLines) {
			start = len(eventLines) - lines
		}
		for _, line := range eventLines[start:] {
			fmt.Println(line)
		}

		if !streaming {
			return
		}

		// Follow mode: poll for new events until interrupted.
		followEvents(c, args[0], result.LastTS)
	},
}

const (
	defaultTailLines   = 20
	followPollInterval = 500 * time.Millisecond
)

// followEvents polls the daemon for new events after lastTS until interrupted.
func followEvents(c *client.Client, agentName string, lastTS int64) {
	fmt.Fprintf(os.Stderr, "following %s (ctrl-c to stop)\n", agentName)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(followPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			fmt.Println() // clean line after ^C
			return
		case <-ticker.C:
			result, err := c.EventsList(agentName, lastTS)
			if err != nil {
				// Non-fatal — daemon may be temporarily unavailable.
				continue
			}
			for _, line := range result.Lines {
				fmt.Println(line)
			}
			if result.LastTS > lastTS {
				lastTS = result.LastTS
			}
		}
	}
}

func init() {
	rootCmd.AddCommand(logsCmd)

	logsCmd.Flags().BoolP("follow", "f", false, "Stream new events as they arrive")
	logsCmd.Flags().BoolP("watch", "w", false, "Stream new events as they arrive (alias for --follow)")
	logsCmd.Flags().IntP("lines", "n", defaultTailLines, "Number of initial lines to show")
	logsCmd.Flags().Bool("raw", false, "Output raw event JSON instead of formatted text")
}
