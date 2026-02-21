package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/baiirun/aetherflow/internal/daemon"
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

var runCommandOutput = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

type opencodeSessionSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
}

type sessionMessage struct {
	Info struct {
		Role string `json:"role"`
	} `json:"info"`
	Parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"parts"`
}

type attachPendingResult struct {
	Success           bool   `json:"success"`
	Code              string `json:"code"`
	State             string `json:"state"`
	SpawnID           string `json:"spawn_id"`
	SessionID         string `json:"session_id,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

func init() {
	rootCmd.AddCommand(sessionsCmd)
	rootCmd.AddCommand(sessionCmd)
	sessionCmd.AddCommand(sessionAttachCmd)

	sessionsCmd.Flags().Bool("json", false, "Output JSON")
	sessionsCmd.Flags().String("server", "", "Filter by server_ref")
	sessionsCmd.Flags().String("session-dir", "", "Session registry directory (overrides config/default)")
	sessionAttachCmd.Flags().String("server", "", "Disambiguate by server_ref when session_id exists on multiple servers")
	sessionAttachCmd.Flags().String("session-dir", "", "Session registry directory (overrides config/default)")
	sessionAttachCmd.Flags().Bool("json", false, "Output pending/error details as JSON")
}

func runSessions(cmd *cobra.Command, _ []string) {
	jsonOut, _ := cmd.Flags().GetBool("json")
	serverFilter, _ := cmd.Flags().GetString("server")

	store, err := openSessionStore(cmd)
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

	sessionIndex := loadOpencodeSessionIndex()
	semanticIndex := loadSessionSemanticIndex(recs, sessionIndex)

	fmt.Printf("%-34s  %-24s  %-10s  %-8s  %-14s  %-14s  %s\n", "SESSION", "SERVER", "STATUS", "ORIGIN", "UPDATED", "WORK", "WHAT")
	for _, r := range recs {
		updated := r.UpdatedAt
		if updated.IsZero() {
			updated = r.CreatedAt
		}
		work := r.WorkRef
		if work == "" {
			work = "-"
		}
		fmt.Printf("%-34s  %-24s  %-10s  %-8s  %-14s  %-14s  %s\n",
			r.SessionID,
			truncateString(r.ServerRef, 24),
			r.Status,
			r.Origin,
			humanSince(updated),
			truncateString(work, 14),
			truncateString(sessionWhatForRecord(r, sessionIndex, semanticIndex), 96),
		)
	}
}

func recordKey(serverRef, sessionID string) string {
	return serverRef + "\x00" + sessionID
}

func loadOpencodeSessionIndex() map[string]opencodeSessionSummary {
	index := make(map[string]opencodeSessionSummary)
	out, err := runCommandOutput("opencode", "session", "list", "--format", "json")
	if err != nil {
		return index
	}
	var items []opencodeSessionSummary
	if err := json.Unmarshal(out, &items); err != nil {
		return index
	}
	for _, item := range items {
		if item.ID == "" {
			continue
		}
		index[item.ID] = item
	}
	return index
}

func loadSessionSemanticIndex(recs []sessions.Record, index map[string]opencodeSessionSummary) map[string]string {
	result := make(map[string]string)
	client := &http.Client{Timeout: 2 * time.Second}
	for _, r := range recs {
		if r.ServerRef == "" || r.SessionID == "" {
			continue
		}
		title := strings.TrimSpace(index[r.SessionID].Title)
		if !shouldEnrichSessionTitle(title) {
			continue
		}
		if what := fetchSessionObjective(client, r.ServerRef, r.SessionID); what != "" {
			result[recordKey(r.ServerRef, r.SessionID)] = what
		}
	}
	return result
}

func shouldEnrichSessionTitle(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return true
	}
	for _, prefix := range []string{
		"autonomous spawn",
		"autonomous agent spawn",
		"spawn-",
		"new session -",
	} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

func fetchSessionObjective(client *http.Client, serverRef, sessionID string) string {
	u := strings.TrimRight(serverRef, "/") + "/session/" + url.PathEscape(sessionID) + "/message"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	var messages []sessionMessage
	if err := json.Unmarshal(body, &messages); err != nil {
		return ""
	}
	for _, msg := range messages {
		if msg.Info.Role != "user" {
			continue
		}
		for _, part := range msg.Parts {
			if part.Type != "text" || strings.TrimSpace(part.Text) == "" {
				continue
			}
			return objectiveFromPromptText(part.Text)
		}
	}
	return ""
}

func objectiveFromPromptText(text string) string {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "\"")
	trimmed = strings.TrimSuffix(trimmed, "\"")
	if trimmed == "" {
		return ""
	}

	if i := strings.Index(trimmed, "## Objective"); i >= 0 {
		sub := trimmed[i+len("## Objective"):]
		sub = strings.TrimSpace(sub)
		if j := strings.Index(sub, "\n## "); j >= 0 {
			sub = sub[:j]
		}
		return squashWhitespace(sub)
	}

	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		return squashWhitespace(l)
	}
	return ""
}

func squashWhitespace(s string) string {
	return strings.TrimSpace(string(bytes.Join(bytes.Fields([]byte(s)), []byte(" "))))
}

func sessionWhatForRecord(r sessions.Record, index map[string]opencodeSessionSummary, semanticIndex map[string]string) string {
	if what := semanticIndex[recordKey(r.ServerRef, r.SessionID)]; what != "" {
		return what
	}
	if summary, ok := index[r.SessionID]; ok {
		title := strings.TrimSpace(summary.Title)
		if title != "" {
			return title
		}
		if summary.Directory != "" {
			return "dir: " + filepath.Base(summary.Directory)
		}
	}
	if r.WorkRef != "" {
		return r.WorkRef
	}
	if r.AgentID != "" {
		return r.AgentID
	}
	if r.Project != "" {
		return r.Project
	}
	if r.Directory != "" {
		return filepath.Base(r.Directory)
	}
	return "-"
}

func runSessionAttach(cmd *cobra.Command, args []string) {
	requestedID := args[0]
	serverFilter, _ := cmd.Flags().GetString("server")
	jsonOut, _ := cmd.Flags().GetBool("json")

	store, err := openSessionStore(cmd)
	if err != nil {
		Fatal("opening session registry: %v", err)
	}
	recs, err := store.List()
	if err != nil {
		Fatal("reading session registry: %v", err)
	}

	matches := make([]sessions.Record, 0, 2)
	for _, r := range recs {
		if r.SessionID != requestedID {
			continue
		}
		if serverFilter != "" && r.ServerRef != serverFilter {
			continue
		}
		matches = append(matches, r)
	}

	if len(matches) == 0 {
		remoteStore, rsErr := openRemoteSpawnStore(cmd)
		if rsErr == nil {
			if rs, getErr := remoteStore.GetBySpawnID(requestedID); getErr == nil && rs != nil {
				if rs.SessionID == "" || rs.State == daemon.RemoteSpawnRequested || rs.State == daemon.RemoteSpawnSpawning || rs.State == daemon.RemoteSpawnUnknown {
					handleAttachPending(jsonOut, rs)
					os.Exit(3)
				}
				if rs.SessionID != "" {
					matches = append(matches, sessions.Record{ServerRef: rs.ServerRef, SessionID: rs.SessionID})
				}
			}
		}
	}

	if len(matches) == 0 {
		if serverFilter != "" {
			Fatal("session or spawn %q not found for server %q", requestedID, serverFilter)
		}
		Fatal("session or spawn %q not found", requestedID)
	}

	if len(matches) > 1 && serverFilter == "" {
		servers := make([]string, 0, len(matches))
		for _, m := range matches {
			servers = append(servers, m.ServerRef)
		}
		sort.Strings(servers)
		Fatal("session %q exists on multiple servers: %s (use --server)", requestedID, strings.Join(servers, ", "))
	}

	target := matches[0]
	if _, err := daemon.ValidateServerURLAttachTarget(target.ServerRef); err != nil {
		Fatal("invalid server_ref %q in session registry: %v", target.ServerRef, err)
	}
	if strings.HasPrefix(target.ServerRef, "-") {
		Fatal("invalid server_ref %q in session registry", target.ServerRef)
	}
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

func handleAttachPending(jsonOut bool, rec *daemon.RemoteSpawnRecord) {
	if jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(attachPendingResult{
			Success:           false,
			Code:              "SESSION_NOT_READY",
			State:             string(rec.State),
			SpawnID:           rec.SpawnID,
			SessionID:         rec.SessionID,
			RetryAfterSeconds: 5,
		})
		return
	}
	fmt.Fprintf(os.Stderr, "SESSION_NOT_READY: spawn %s is %s; retry in ~5s\n", rec.SpawnID, rec.State)
}

func openSessionStore(cmd *cobra.Command) (*sessions.Store, error) {
	sessionDir, _ := cmd.Flags().GetString("session-dir")
	if sessionDir != "" {
		return sessions.Open(sessionDir)
	}

	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = ".aetherflow.yaml"
	}
	var cfg daemon.Config
	_ = daemon.LoadConfigFile(configPath, &cfg)
	return sessions.Open(cfg.SessionDir)
}

func openRemoteSpawnStore(cmd *cobra.Command) (*daemon.RemoteSpawnStore, error) {
	sessionDir, _ := cmd.Flags().GetString("session-dir")
	if sessionDir != "" {
		return daemon.OpenRemoteSpawnStore(sessionDir)
	}

	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = ".aetherflow.yaml"
	}
	var cfg daemon.Config
	_ = daemon.LoadConfigFile(configPath, &cfg)
	return daemon.OpenRemoteSpawnStore(cfg.SessionDir)
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
