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

// sessionListEntry is the unified display/JSON shape for `af sessions`.
// It embeds a sessions.Record with an optional SpawnID field so remote
// spawns that don't yet have a session_id are visible in the listing.
type sessionListEntry struct {
	sessions.Record
	SpawnID  string `json:"spawn_id,omitempty"`
	Provider string `json:"provider,omitempty"`
}

type attachPendingResult struct {
	Success           bool   `json:"success"`
	Code              string `json:"code"`
	State             string `json:"state"`
	SpawnID           string `json:"spawn_id"`
	SessionID         string `json:"session_id,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

type attachErrorResult struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	State   string `json:"state,omitempty"`
	SpawnID string `json:"spawn_id,omitempty"`
	Error   string `json:"error"`
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

	// Merge remote spawn records that aren't already represented.
	var remoteRecs []daemon.RemoteSpawnRecord
	if remoteStore, rsErr := openRemoteSpawnStore(cmd); rsErr == nil {
		var listErr error
		remoteRecs, listErr = remoteStore.List()
		if listErr != nil {
			fmt.Fprintf(os.Stderr, "warning: reading remote spawn store: %v\n", listErr)
		}
	}
	entries := buildSessionListEntries(recs, remoteRecs, serverFilter)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(entries)
		return
	}

	if len(entries) == 0 {
		fmt.Println("no sessions found")
		return
	}

	// Extract plain records for semantic index (remote-spawn-only entries
	// have empty SessionID so they'll be skipped by the enrichment).
	plainRecs := make([]sessions.Record, len(entries))
	for i := range entries {
		plainRecs[i] = entries[i].Record
	}
	sessionIndex := loadOpencodeSessionIndex()
	semanticIndex := loadSessionSemanticIndex(plainRecs, sessionIndex)

	fmt.Printf("%-34s  %-24s  %-10s  %-8s  %-14s  %-14s  %s\n", "SESSION", "SERVER", "STATUS", "ORIGIN", "UPDATED", "WORK", "WHAT")
	for _, e := range entries {
		updated := e.UpdatedAt
		if updated.IsZero() {
			updated = e.CreatedAt
		}
		work := e.WorkRef
		if work == "" {
			work = "-"
		}
		// Show spawn_id in the SESSION column when session_id is unknown.
		displayID := e.SessionID
		if displayID == "" && e.SpawnID != "" {
			const pendingSuffix = " (pending)"
			displayID = truncate(e.SpawnID, 34-len(pendingSuffix)) + pendingSuffix
		}
		what := sessionWhatForEntry(e, sessionIndex, semanticIndex)
		fmt.Printf("%-34s  %-24s  %-10s  %-8s  %-14s  %-14s  %s\n",
			truncate(displayID, 34),
			truncate(e.ServerRef, 24),
			e.Status,
			e.Origin,
			humanSince(updated),
			truncate(work, 14),
			truncate(what, 96),
		)
	}
}

// buildSessionListEntries merges session records with remote spawn records
// into a unified, sorted list. Remote spawns that already have a session_id
// matching an existing session record are skipped (the session record wins).
func buildSessionListEntries(recs []sessions.Record, remoteRecs []daemon.RemoteSpawnRecord, serverFilter string) []sessionListEntry {
	// Index session IDs already present.
	seenSessionIDs := make(map[string]struct{}, len(recs))
	for _, r := range recs {
		if r.SessionID != "" {
			seenSessionIDs[r.SessionID] = struct{}{}
		}
	}

	// Start with session records (applying server filter if set).
	entries := make([]sessionListEntry, 0, len(recs)+len(remoteRecs))
	for _, r := range recs {
		if serverFilter != "" && r.ServerRef != serverFilter {
			continue
		}
		entries = append(entries, sessionListEntry{Record: r})
	}

	// Merge remote spawn records that aren't already represented.
	for _, rs := range remoteRecs {
		// Skip if the session is already in the session registry.
		if _, seen := seenSessionIDs[rs.SessionID]; rs.SessionID != "" && seen {
			continue
		}
		// Apply server filter if set. Records with empty ServerRef are
		// excluded when a filter is active — they haven't been assigned
		// a server yet and don't match any specific filter.
		if serverFilter != "" && rs.ServerRef != serverFilter {
			continue
		}
		entries = append(entries, sessionListEntry{
			Record: sessions.Record{
				ServerRef: rs.ServerRef,
				SessionID: rs.SessionID,
				Origin:    sessions.OriginSpawn,
				Status:    remoteSpawnStatusToSessionStatus(rs.State),
				CreatedAt: rs.CreatedAt,
				UpdatedAt: rs.UpdatedAt,
			},
			SpawnID:  rs.SpawnID,
			Provider: rs.Provider,
		})
	}

	// Sort by UpdatedAt descending (most recent first).
	// Use CreatedAt as fallback for entries with zero UpdatedAt.
	// SliceStable preserves insertion order for equal timestamps.
	sort.SliceStable(entries, func(i, j int) bool {
		ti := entries[i].UpdatedAt
		if ti.IsZero() {
			ti = entries[i].CreatedAt
		}
		tj := entries[j].UpdatedAt
		if tj.IsZero() {
			tj = entries[j].CreatedAt
		}
		return ti.After(tj)
	})

	return entries
}

// remoteSpawnStatusToSessionStatus maps remote spawn states to session
// display statuses per the plan:
//   - requested|spawning|unknown → "pending"
//   - running → "active"
//   - failed|terminated → "inactive"
func remoteSpawnStatusToSessionStatus(state daemon.RemoteSpawnState) sessions.Status {
	switch state {
	case daemon.RemoteSpawnRequested, daemon.RemoteSpawnSpawning, daemon.RemoteSpawnUnknown:
		return sessions.StatusPending
	case daemon.RemoteSpawnRunning:
		return sessions.StatusActive
	case daemon.RemoteSpawnFailed, daemon.RemoteSpawnTerminated:
		return sessions.StatusInactive
	default:
		return sessions.StatusPending
	}
}

// sessionWhatForEntry returns the display string for the WHAT column.
// Delegates to sessionWhatForRecord for session-backed entries, and
// shows provider info for remote-spawn-only entries.
func sessionWhatForEntry(e sessionListEntry, index map[string]opencodeSessionSummary, semanticIndex map[string]string) string {
	// If there's a session ID, use the existing logic.
	if e.SessionID != "" {
		return sessionWhatForRecord(e.Record, index, semanticIndex)
	}
	// Remote spawn without a session yet — show provider context.
	if e.Provider != "" {
		return e.Provider + ": " + e.SpawnID
	}
	if e.SpawnID != "" {
		return e.SpawnID
	}
	return "-"
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
		// Only enrich local sessions — remote hosts don't expose the
		// opencode session message API.
		if u, parseErr := url.Parse(r.ServerRef); parseErr != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
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
		if rsErr != nil {
			handleAttachError(jsonOut, "REMOTE_SPAWN_STORE_ERROR", nil, rsErr)
			os.Exit(1)
		}
		rs, getErr := remoteStore.GetBySpawnID(requestedID)
		if getErr != nil {
			handleAttachError(jsonOut, "REMOTE_SPAWN_STORE_ERROR", nil, getErr)
			os.Exit(1)
		}
		if rs != nil {
			if serverFilter != "" && rs.ServerRef != "" && rs.ServerRef != serverFilter {
				handleAttachError(jsonOut, "SERVER_FILTER_MISMATCH", rs, fmt.Errorf("spawn %s maps to server %s, not %s", rs.SpawnID, rs.ServerRef, serverFilter))
				os.Exit(1)
			}
			if daemon.IsRemoteSpawnPending(rs) || (rs.State == daemon.RemoteSpawnRunning && rs.SessionID == "") {
				handleAttachPending(jsonOut, rs)
				os.Exit(3)
			}
			if daemon.IsRemoteSpawnTerminal(rs) && rs.SessionID == "" {
				// Show a sanitized message — full provider error stays in
				// remote_spawns.json for debugging.
				handleAttachError(jsonOut, "SESSION_NOT_AVAILABLE", rs, fmt.Errorf("spawn %s is %s (see remote_spawns.json for details)", rs.SpawnID, rs.State))
				os.Exit(1)
			}
			if rs.SessionID != "" {
				matches = append(matches, sessions.Record{ServerRef: rs.ServerRef, SessionID: rs.SessionID})
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
	if !daemon.IsValidSessionID(target.SessionID) {
		Fatal("invalid session_id %q in session registry", target.SessionID)
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

func handleAttachError(jsonOut bool, code string, rec *daemon.RemoteSpawnRecord, err error) {
	errMsg := code
	if err != nil {
		errMsg = err.Error()
	}
	result := attachErrorResult{Success: false, Code: code, Error: errMsg}
	if rec != nil {
		result.State = string(rec.State)
		result.SpawnID = rec.SpawnID
	}
	if jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(result)
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", code, err)
}

func openSessionStore(cmd *cobra.Command) (*sessions.Store, error) {
	sessionDir := resolveSessionDir(cmd)
	return sessions.Open(sessionDir)
}

func openRemoteSpawnStore(cmd *cobra.Command) (*daemon.RemoteSpawnStore, error) {
	return daemon.OpenRemoteSpawnStore(resolveSessionDir(cmd))
}

func resolveSessionDir(cmd *cobra.Command) string {
	sessionDir, _ := cmd.Flags().GetString("session-dir")
	if sessionDir != "" {
		return sessionDir
	}

	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = ".aetherflow.yaml"
	}
	var cfg daemon.Config
	_ = daemon.LoadConfigFile(configPath, &cfg)
	return cfg.SessionDir
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
