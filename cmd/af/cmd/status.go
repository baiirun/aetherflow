package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/geobrowser/aetherflow/internal/client"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show swarm overview",
	Long: `Show the current state of the agent pool, including active agents
and their task progress, plus the pending task queue.

Each agent row shows:
  name      task_id   uptime   role   last activity from prog log

Requires a running daemon.`,
	Run: func(cmd *cobra.Command, args []string) {
		socketPath, _ := cmd.Flags().GetString("socket")
		asJSON, _ := cmd.Flags().GetBool("json")

		c := client.New(socketPath)
		status, err := c.StatusFull()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			fmt.Fprintf(os.Stderr, "\nIs the daemon running? Start it with: af daemon start --project <name>\n")
			os.Exit(1)
		}

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(status)
			return
		}

		printStatus(status)
	},
}

func printStatus(s *client.FullStatus) {
	active := len(s.Agents)
	idle := s.PoolSize - active

	fmt.Printf("Pool: %d/%d active", active, s.PoolSize)
	if s.Project != "" {
		fmt.Printf("  (%s)", s.Project)
	}
	fmt.Println()

	if active > 0 {
		fmt.Println()
		for _, a := range s.Agents {
			uptime := formatUptime(a.SpawnTime)
			summary := a.LastLog
			if summary == "" {
				summary = a.TaskTitle
			}
			summary = stripANSI(summary)
			summary = truncate(summary, 50)

			fmt.Printf("  %-14s %-10s %6s  %-8s %s\n",
				a.ID,
				a.TaskID,
				uptime,
				a.Role,
				quote(summary),
			)
		}
	}

	if idle > 0 {
		fmt.Printf("  + %d idle\n", idle)
	}

	fmt.Println()

	if len(s.Queue) > 0 {
		fmt.Printf("Queue: %d pending\n", len(s.Queue))
		for _, t := range s.Queue {
			title := stripANSI(t.Title)
			title = truncate(title, 40)
			fmt.Printf("  %-10s P%d  %s\n", t.ID, t.Priority, quote(title))
		}
	} else {
		fmt.Println("Queue: empty")
	}

	if len(s.Errors) > 0 {
		fmt.Println()
		fmt.Printf("Warnings: %d\n", len(s.Errors))
		for _, e := range s.Errors {
			fmt.Printf("  ! %s\n", e)
		}
	}
}

// formatUptime returns a human-readable duration since the given spawn time.
func formatUptime(spawnTime time.Time) string {
	if spawnTime.IsZero() {
		return "?"
	}
	d := time.Since(spawnTime)

	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%dh", days, h)
	}
}

// truncate shortens s to max runes, appending an ellipsis if truncated.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "\u2026"
}

// quote wraps a non-empty string in double quotes for display.
func quote(s string) string {
	if s == "" {
		return ""
	}
	return `"` + s + `"`
}

// ansiEscape matches ANSI escape sequences: CSI sequences, OSC sequences, and charset selection.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\(B`)

// stripANSI removes ANSI escape sequences from s to prevent terminal injection.
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

func init() {
	rootCmd.AddCommand(statusCmd)

	statusCmd.Flags().String("socket", "", "Unix socket path (default: /tmp/aetherd.sock)")
	statusCmd.Flags().Bool("json", false, "Output raw JSON")
}
