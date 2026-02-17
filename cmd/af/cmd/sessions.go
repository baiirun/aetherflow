package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/baiirun/aetherflow/internal/sessions"
	"github.com/spf13/cobra"
)

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List known opencode sessions",
	Long: `List session records from aetherflow's global session registry.

The registry tracks routing metadata ({server_ref, session_id}) and origin
context so sessions can be resumed independently of task backends.`,
	Run: runSessions,
}

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Session-oriented operations",
}

var sessionAttachCmd = &cobra.Command{
	Use:   "attach <session-id>",
	Short: "Attach interactively to a known session",
	Args:  cobra.ExactArgs(1),
	Run:   runSessionAttach,
}

func init() {
	rootCmd.AddCommand(sessionsCmd)
	rootCmd.AddCommand(sessionCmd)
	sessionCmd.AddCommand(sessionAttachCmd)

	sessionsCmd.Flags().Bool("json", false, "Output JSON")
	sessionsCmd.Flags().String("server", "", "Filter by server_ref")
	sessionAttachCmd.Flags().String("server", "", "Disambiguate by server_ref when session_id exists on multiple servers")
}

func runSessions(cmd *cobra.Command, _ []string) {
	jsonOut, _ := cmd.Flags().GetBool("json")
	serverFilter, _ := cmd.Flags().GetString("server")

	store, err := sessions.Open("")
	if err != nil {
		Fatal("opening session registry: %v", err)
	}
	recs, err := store.List()
	if err != nil {
		Fatal("reading session registry: %v", err)
	}

	if serverFilter != "" {
		filtered := recs[:0]
		for _, r := range recs {
			if r.ServerRef == serverFilter {
				filtered = append(filtered, r)
			}
		}
		recs = filtered
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(recs)
		return
	}

	if len(recs) == 0 {
		fmt.Println("no sessions found")
		return
	}

	fmt.Printf("%-34s  %-24s  %-10s  %-8s  %-14s  %s\n", "SESSION", "SERVER", "STATUS", "ORIGIN", "UPDATED", "WORK")
	for _, r := range recs {
		updated := r.UpdatedAt
		if updated.IsZero() {
			updated = r.CreatedAt
		}
		work := r.WorkRef
		if work == "" {
			work = "-"
		}
		fmt.Printf("%-34s  %-24s  %-10s  %-8s  %-14s  %s\n",
			r.SessionID,
			truncateString(r.ServerRef, 24),
			r.Status,
			r.Origin,
			humanSince(updated),
			work,
		)
	}
}

func runSessionAttach(cmd *cobra.Command, args []string) {
	sessionID := args[0]
	serverFilter, _ := cmd.Flags().GetString("server")

	store, err := sessions.Open("")
	if err != nil {
		Fatal("opening session registry: %v", err)
	}
	recs, err := store.List()
	if err != nil {
		Fatal("reading session registry: %v", err)
	}

	matches := make([]sessions.Record, 0, 2)
	for _, r := range recs {
		if r.SessionID != sessionID {
			continue
		}
		if serverFilter != "" && r.ServerRef != serverFilter {
			continue
		}
		matches = append(matches, r)
	}

	if len(matches) == 0 {
		if serverFilter != "" {
			Fatal("session %q not found for server %q", sessionID, serverFilter)
		}
		Fatal("session %q not found", sessionID)
	}

	if len(matches) > 1 && serverFilter == "" {
		servers := make([]string, 0, len(matches))
		for _, m := range matches {
			servers = append(servers, m.ServerRef)
		}
		sort.Strings(servers)
		Fatal("session %q exists on multiple servers: %s (use --server)", sessionID, strings.Join(servers, ", "))
	}

	target := matches[0]
	attach := exec.Command("opencode", "attach", target.ServerRef, "--session", target.SessionID)
	attach.Stdin = os.Stdin
	attach.Stdout = os.Stdout
	attach.Stderr = os.Stderr

	if err := attach.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		Fatal("running opencode attach: %v", err)
	}
}

func humanSince(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t).Round(time.Second)
	if d < time.Minute {
		return d.String()
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Format("2006-01-02")
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
