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
	Use:   "status [agent-name]",
	Short: "Show swarm overview or agent detail",
	Long: `Show the current state of the agent pool, or drill into a single agent.

Without arguments, shows the swarm overview:
  Pool utilization, active agents with uptime and activity, pending queue.

With an agent name, shows detailed agent info:
  Task details, uptime, last prog log, and recent tool call history
  parsed from the agent's JSONL log.

Requires a running daemon.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		socketPath, _ := cmd.Flags().GetString("socket")
		asJSON, _ := cmd.Flags().GetBool("json")

		c := client.New(socketPath)

		if len(args) == 1 {
			runStatusAgent(c, args[0], asJSON, cmd)
			return
		}

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

func runStatusAgent(c *client.Client, agentName string, asJSON bool, cmd *cobra.Command) {
	limit, _ := cmd.Flags().GetInt("limit")
	detail, err := c.StatusAgent(agentName, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(detail)
		return
	}

	printAgentDetail(detail)
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

// unsafeChars strips ANSI escape sequences and C0 control characters.
//
// Sequence types handled:
//   - CSI: \x1b[ ... <letter>         (colors, cursor movement, clear screen)
//   - OSC: \x1b] ... \x07 or \x1b\\   (window title, hyperlinks)
//   - DCS/PM/APC: \x1bP/^/_ ... ST    (device control, privacy messages)
//   - Two-char: \x1b<char>            (charset selection like \x1b(B)
//   - C0 controls except \t and \n    (CR, BS, DEL, NUL, etc.)
var unsafeChars = regexp.MustCompile(
	`\x1b\[[0-9;]*[a-zA-Z]` + // CSI sequences
		`|\x1b\][^\x07]*(?:\x07|\x1b\\)` + // OSC sequences (BEL or ST terminated)
		`|\x1b[P^_][^\x1b]*\x1b\\` + // DCS/PM/APC sequences (ST terminated)
		`|\x1b[^\[P^_\]]` + // Two-char escape sequences
		`|\r|\x08|\x7f` + // CR, BS, DEL
		`|[\x00-\x08\x0b\x0c\x0e-\x1a]`, // Other C0 controls (keep \t=0x09, \n=0x0a, ESC=0x1b)
)

// stripANSI removes ANSI escape sequences and unsafe control characters from s
// to prevent terminal injection.
func stripANSI(s string) string {
	return unsafeChars.ReplaceAllString(s, "")
}

func printAgentDetail(d *client.AgentDetail) {
	uptime := formatUptime(d.SpawnTime)

	fmt.Printf("Agent: %s\n", d.ID)
	fmt.Printf("  Task:    %s", d.TaskID)
	if d.TaskTitle != "" {
		fmt.Printf("  %s", quote(stripANSI(d.TaskTitle)))
	}
	fmt.Println()
	fmt.Printf("  Role:    %s\n", d.Role)
	fmt.Printf("  PID:     %d\n", d.PID)
	fmt.Printf("  Uptime:  %s\n", uptime)

	if d.LastLog != "" {
		fmt.Printf("  Activity: %s\n", quote(stripANSI(truncate(d.LastLog, 70))))
	}

	fmt.Println()

	if len(d.ToolCalls) == 0 {
		fmt.Println("Tool calls: none")
	} else {
		fmt.Printf("Tool calls: %d recent\n", len(d.ToolCalls))
		fmt.Println()
		for _, tc := range d.ToolCalls {
			relTime := formatRelativeTime(tc.Timestamp)
			input := stripANSI(truncate(tc.Input, 60))

			dur := ""
			if tc.DurationMs > 0 {
				dur = fmt.Sprintf(" (%dms)", tc.DurationMs)
			}

			title := ""
			if tc.Title != "" {
				title = " " + stripANSI(tc.Title)
			}

			fmt.Printf("  %6s  %-10s%s %s%s\n", relTime, tc.Tool, title, input, dur)
		}
	}

	if len(d.Errors) > 0 {
		fmt.Println()
		fmt.Printf("Warnings: %d\n", len(d.Errors))
		for _, e := range d.Errors {
			fmt.Printf("  ! %s\n", e)
		}
	}
}

// formatRelativeTime returns a human-readable relative time string.
func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	if d < 0 {
		return "now"
	}

	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh ago", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

func init() {
	rootCmd.AddCommand(statusCmd)

	statusCmd.Flags().String("socket", "", "Unix socket path (default: /tmp/aetherd.sock)")
	statusCmd.Flags().Bool("json", false, "Output raw JSON")
	statusCmd.Flags().Int("limit", 20, "Max tool calls to show in agent detail view")
}
