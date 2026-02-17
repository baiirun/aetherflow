package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/baiirun/aetherflow/internal/client"
	"github.com/baiirun/aetherflow/internal/term"
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

Use -w/--watch or -f/--follow for continuous monitoring (refreshes every 2s by default).

Requires a running daemon.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		socketPath := resolveSocketPath(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")
		watch, _ := cmd.Flags().GetBool("watch")
		follow, _ := cmd.Flags().GetBool("follow")
		interval, _ := cmd.Flags().GetDuration("interval")

		// Both --watch and --follow enable streaming; treat them as aliases.
		streaming := watch || follow

		c := client.New(socketPath)

		if !streaming {
			runStatusOnce(c, args, asJSON, cmd)
			return
		}

		// Streaming mode: re-render on interval until interrupted.
		if asJSON {
			fmt.Fprintf(os.Stderr, "error: streaming mode (--watch/--follow) and --json cannot be combined\n")
			os.Exit(1)
		}

		runStatusWatch(c, args, interval, cmd)
	},
}

// runStatusOnce fetches and prints status a single time.
func runStatusOnce(c *client.Client, args []string, asJSON bool, cmd *cobra.Command) {
	if len(args) == 1 {
		runStatusAgent(c, args[0], asJSON, cmd)
		return
	}

	status, err := c.StatusFull()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nIs the daemon running? Start it with: af daemon start --project <name>\n")
		fmt.Fprintf(os.Stderr, "Hint: socket path is derived from project in .aetherflow.yaml\n")
		os.Exit(1)
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(status)
		return
	}

	printStatus(status)
}

const minWatchInterval = 500 * time.Millisecond

// runStatusWatch polls the daemon on an interval, clearing the screen between renders.
// It exits cleanly on SIGINT or SIGTERM (Ctrl+C or process manager stop).
func runStatusWatch(c *client.Client, args []string, interval time.Duration, cmd *cobra.Command) {
	if interval < minWatchInterval {
		fmt.Fprintf(os.Stderr, "error: --interval must be at least %s\n", minWatchInterval)
		os.Exit(1)
	}

	// Read flags once — they don't change between ticks.
	limit, _ := cmd.Flags().GetInt("limit")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Render immediately, then on each tick.
	for {
		clearScreen()

		if len(args) == 1 {
			detail, err := c.StatusAgent(args[0], limit)
			if err != nil {
				fmt.Printf("error: %v\n", err)
			} else {
				printAgentDetail(detail)
			}
		} else {
			status, err := c.StatusFull()
			if err != nil {
				fmt.Printf("error: %v\n", err)
			} else {
				printStatus(status)
			}
		}

		fmt.Printf("\nRefreshing every %s. Press Ctrl+C to exit.", interval)

		select {
		case <-sigCh:
			fmt.Println() // clean line after ^C
			return
		case <-ticker.C:
		}
	}
}

// clearScreen moves the cursor to the top-left and clears the terminal.
// Uses raw ANSI escape sequences intentionally — cursor control is needed
// even when --no-color is set, since watch mode requires screen clearing
// regardless of color preference.
func clearScreen() {
	fmt.Print("\x1b[H\x1b[2J")
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
		_ = enc.Encode(detail)
		return
	}

	printAgentDetail(detail)
}

// Column widths for the agent table in printStatus.
// PadRight uses these to pad visible content before wrapping in color,
// so ANSI codes don't throw off alignment.
const (
	colID     = 14
	colTask   = 10
	colUptime = 6
	colRole   = 8
	// 2 indent + colID + 1 space + colTask + 1 space + colUptime + 2 spaces + colRole + 1 space.
	agentRowPrefix = 2 + colID + 1 + colTask + 1 + colUptime + 2 + colRole + 1
)

func printStatus(s *client.FullStatus) {
	active := len(s.Agents)
	idle := s.PoolSize - active

	// Pool header: "Pool: 2/3 active" with utilization color.
	utilization := term.Greenf("%d/%d active", active, s.PoolSize)
	if active == 0 {
		utilization = term.Dimf("%d/%d active", active, s.PoolSize)
	}
	fmt.Printf("%s %s", term.Bold("Pool:"), utilization)

	if s.PoolMode != "" && s.PoolMode != "active" {
		fmt.Printf("  %s", term.Yellowf("[%s]", s.PoolMode))
	}
	if policy := s.NormalizedSpawnPolicy(); policy != client.SpawnPolicyAuto {
		fmt.Printf("  %s", term.Yellowf("[spawn:%s]", policy))
	}
	if s.Project != "" {
		fmt.Printf("  %s", term.Dimf("(%s)", s.Project))
	}
	fmt.Println()

	if active > 0 {
		width := term.Width(100)
		summaryMax := width - agentRowPrefix
		if summaryMax < 20 {
			summaryMax = 20
		}

		fmt.Println()
		for _, a := range s.Agents {
			uptime := formatUptime(a.SpawnTime)
			summary := a.LastLog
			if summary == "" {
				summary = a.TaskTitle
			}
			summary = truncate(stripANSI(summary), summaryMax)

			fmt.Printf("  %s %s %s  %s %s\n",
				term.PadRight(a.ID, colID, term.Cyan),
				term.PadRight(a.TaskID, colTask, term.Blue),
				term.PadLeft(uptime, colUptime, term.Green),
				term.PadRight(a.Role, colRole, term.Magenta),
				term.Dim(quote(summary)),
			)
		}
	}

	if idle > 0 {
		fmt.Printf("  %s\n", term.Dimf("+ %d idle", idle))
	}

	fmt.Println()

	// Show spawned agents (outside the pool).
	if len(s.Spawns) > 0 {
		fmt.Printf("%s %s\n", term.Bold("Spawns:"), term.Cyan(fmt.Sprintf("%d running", len(s.Spawns))))
		width := term.Width(100)
		promptMax := width - 2 - colID - 1 - colUptime - 2
		if promptMax < 20 {
			promptMax = 20
		}
		for _, sp := range s.Spawns {
			uptime := formatUptime(sp.SpawnTime)
			prompt := truncate(stripANSI(sp.Prompt), promptMax)
			fmt.Printf("  %s %s  %s\n",
				term.PadRight(sp.SpawnID, colID, term.Cyan),
				term.PadLeft(uptime, colUptime, term.Green),
				term.Dim(quote(prompt)),
			)
		}
		fmt.Println()
	}

	if len(s.Queue) > 0 {
		fmt.Printf("%s %s\n", term.Bold("Queue:"), term.Yellowf("%d pending", len(s.Queue)))
		for _, t := range s.Queue {
			title := truncate(stripANSI(t.Title), 40)
			fmt.Printf("  %s %s  %s\n",
				term.PadRight(t.ID, colTask, term.Blue),
				term.Yellowf("P%d", t.Priority),
				term.Yellow(quote(title)),
			)
		}
	} else {
		fmt.Printf("%s %s\n", term.Bold("Queue:"), term.Dim("empty"))
	}

	if len(s.Errors) > 0 {
		fmt.Println()
		fmt.Printf("%s %s\n", term.Bold("Warnings:"), term.Redf("%d", len(s.Errors)))
		for _, e := range s.Errors {
			fmt.Printf("  %s %s\n", term.Red("!"), stripANSI(e))
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

	fmt.Printf("%s %s\n", term.Bold("Agent:"), term.Cyan(d.ID))
	fmt.Printf("  %s %s", term.Bold("Task:"), term.Blue(d.TaskID))
	if d.TaskTitle != "" {
		fmt.Printf("  %s", term.Dim(quote(stripANSI(d.TaskTitle))))
	}
	fmt.Println()
	fmt.Printf("  %s %s\n", term.Bold("Role:"), term.Magenta(d.Role))
	fmt.Printf("  %s %d\n", term.Bold("PID:"), d.PID)
	fmt.Printf("  %s %s\n", term.Bold("Uptime:"), term.Green(uptime))

	if d.LastLog != "" {
		fmt.Printf("  %s %s\n", term.Bold("Activity:"), term.Dim(quote(truncate(stripANSI(d.LastLog), 70))))
	}

	fmt.Println()

	if len(d.ToolCalls) == 0 {
		fmt.Printf("%s %s\n", term.Bold("Tool calls:"), term.Dim("none"))
	} else {
		fmt.Printf("%s %d recent\n", term.Bold("Tool calls:"), len(d.ToolCalls))
		fmt.Println()
		for _, tc := range d.ToolCalls {
			relTime := formatRelativeTime(tc.Timestamp)
			input := truncate(stripANSI(tc.Input), 60)

			dur := ""
			if tc.DurationMs > 0 {
				dur = term.Dimf(" (%dms)", tc.DurationMs)
			}

			title := ""
			if tc.Title != "" {
				title = " " + term.Dim(stripANSI(tc.Title))
			}

			fmt.Printf("  %s  %s%s %s%s\n",
				term.PadLeft(relTime, 6, term.Dim),
				term.PadRight(tc.Tool, 10, term.Cyan),
				title,
				input,
				dur,
			)
		}
	}

	if len(d.Errors) > 0 {
		fmt.Println()
		fmt.Printf("%s %s\n", term.Bold("Warnings:"), term.Redf("%d", len(d.Errors)))
		for _, e := range d.Errors {
			fmt.Printf("  %s %s\n", term.Red("!"), stripANSI(e))
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

	statusCmd.Flags().Bool("json", false, "Output raw JSON")
	statusCmd.Flags().Int("limit", 20, "Max tool calls to show in agent detail view")
	statusCmd.Flags().BoolP("watch", "w", false, "Continuously refresh the display")
	statusCmd.Flags().BoolP("follow", "f", false, "Continuously refresh the display (alias for --watch)")
	statusCmd.Flags().Duration("interval", 2*time.Second, "Refresh interval for streaming mode")
}
