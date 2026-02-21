package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/baiirun/aetherflow/internal/client"
	"github.com/baiirun/aetherflow/internal/daemon"
	"github.com/baiirun/aetherflow/internal/protocol"
	"github.com/baiirun/aetherflow/internal/term"
	"github.com/spf13/cobra"
)

var spawnCmd = &cobra.Command{
	Use:   "spawn <prompt>",
	Short: "Spawn a one-off autonomous agent",
	Long: `Spawn an agent with a freeform prompt that runs to completion.

The agent works in an isolated git worktree, implements the prompt,
and creates a PR (or merges to main in --solo mode).

No daemon or prog task required — the prompt is the spec, the PR is
the deliverable.

Examples:
  af spawn "refactor the auth module to use JWT"
  af spawn "add rate limiting to the /api/users endpoint" --solo
  af spawn "fix the flaky TestRetry test" -d`,
	Args: cobra.ExactArgs(1),
	Run:  runSpawn,
}

func init() {
	rootCmd.AddCommand(spawnCmd)

	f := spawnCmd.Flags()
	f.BoolP("detach", "d", false, "Run in background")
	f.Bool("json", false, "Output spawn metadata as JSON (for programmatic consumption)")
	f.Bool("solo", false, "Solo mode: agent merges to main instead of creating a PR")
	f.String("spawn-cmd", daemon.DefaultSpawnCmd, "Command to launch the agent session")
	f.String("prompt-dir", "", "Override embedded prompts with files from this directory")
	f.String("provider", "local", "Spawn provider: local or sprites")
	f.String("request-id", "", "Optional idempotency key for provider-backed spawns")
}

func runSpawn(cmd *cobra.Command, args []string) {
	userPrompt := args[0]

	detach, _ := cmd.Flags().GetBool("detach")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	solo, _ := cmd.Flags().GetBool("solo")
	provider, _ := cmd.Flags().GetString("provider")
	requestID, _ := cmd.Flags().GetString("request-id")
	spawnCmd, _ := cmd.Flags().GetString("spawn-cmd")
	promptDir, _ := cmd.Flags().GetString("prompt-dir")

	// Load config file values for fields not set by flags.
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = ".aetherflow.yaml"
	}
	var fileCfg daemon.Config
	_ = daemon.LoadConfigFile(configPath, &fileCfg) // ignore missing file

	if !cmd.Flags().Changed("spawn-cmd") && fileCfg.SpawnCmd != "" {
		spawnCmd = fileCfg.SpawnCmd
	}
	if !cmd.Flags().Changed("solo") && fileCfg.Solo {
		solo = true
	}
	if !cmd.Flags().Changed("prompt-dir") && fileCfg.PromptDir != "" {
		promptDir = fileCfg.PromptDir
	}

	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "local" && provider != "sprites" {
		Fatal("invalid provider %q (expected one of: local, sprites)", provider)
	}

	if provider == "sprites" {
		if requestID == "" {
			var err error
			requestID, err = newRequestID()
			if err != nil {
				Fatal("generating request-id: %v", err)
			}
		}
		spawnID := newSpawnID()
		runSpritesSpawn(cmd, spawnID, requestID, userPrompt, jsonOutput, fileCfg)
		return
	}

	// Phase A server-first launch path: ensure attach-based spawn command.
	serverURL := fileCfg.ServerURL
	if serverURL == "" {
		serverURL = daemon.DefaultServerURL
	}
	if _, err := daemon.ValidateServerURLLocal(serverURL); err != nil {
		Fatal("invalid server URL: %v", err)
	}
	spawnCmd = daemon.EnsureAttachSpawnCmd(spawnCmd, serverURL)

	// Generate a unique spawn ID for worktree/branch naming.
	// The "spawn-" prefix ensures no collision with pool agent IDs.
	// A random hex suffix expands the namespace from ~14K to ~943M
	// combinations, making birthday-paradox collisions negligible.
	spawnID := newSpawnID()

	// Render the spawn prompt.
	prompt, err := daemon.RenderSpawnPrompt(promptDir, userPrompt, spawnID, solo)
	if err != nil {
		Fatal("rendering prompt: %v", err)
	}

	// Resolve the daemon socket for best-effort registration.
	socketPath := resolveSocketPath(cmd)

	if detach {
		runDetached(spawnID, userPrompt, spawnCmd, prompt, socketPath, jsonOutput)
		return
	}

	runForeground(spawnID, userPrompt, spawnCmd, prompt, socketPath, jsonOutput)
}

// newSpawnID generates a unique spawn identifier.
// Format: spawn-<adjective>_<noun>-<4hex> (e.g., "spawn-ghost_wolf-a3f2").
func newSpawnID() string {
	name := protocol.GenerateAgentName()
	suffix := make([]byte, 2)
	if _, err := rand.Read(suffix); err != nil {
		// Fallback: should never happen, but don't crash.
		return "spawn-" + name
	}
	return fmt.Sprintf("spawn-%s-%x", name, suffix)
}

func newRequestID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto random read failed: %w", err)
	}
	return "req-" + hex.EncodeToString(b), nil
}

func runSpritesSpawn(cmd *cobra.Command, spawnID, requestID, userPrompt string, jsonOutput bool, cfg daemon.Config) {
	token := strings.TrimSpace(os.Getenv("SPRITES_TOKEN"))
	if token == "" {
		if jsonOutput {
			fatalSpawnJSON("MISSING_TOKEN", spawnID, fmt.Errorf("sprites provider requires SPRITES_TOKEN"))
		}
		Fatal("sprites provider requires SPRITES_TOKEN")
	}

	store, err := daemon.OpenRemoteSpawnStore(cfg.SessionDir)
	if err != nil {
		if jsonOutput {
			fatalSpawnJSON("STORE_ERROR", spawnID, fmt.Errorf("opening remote spawn store: %w", err))
		}
		Fatal("opening remote spawn store: %v", err)
	}
	rec := daemon.RemoteSpawnRecord{
		SpawnID:   spawnID,
		Provider:  "sprites",
		RequestID: requestID,
		State:     daemon.RemoteSpawnRequested,
	}
	if err := store.Upsert(rec); err != nil {
		if daemon.IsIdempotencyConflict(err) {
			existing, lookupErr := store.GetByProviderRequest("sprites", requestID)
			if lookupErr != nil {
				if jsonOutput {
					fatalSpawnJSON("IDEMPOTENCY_LOOKUP_ERROR", spawnID, fmt.Errorf("resolving idempotency conflict: %w", lookupErr))
				}
				Fatal("resolving idempotency conflict: %v", lookupErr)
			}
			if existing == nil {
				if jsonOutput {
					fatalSpawnJSON("IDEMPOTENCY_LOOKUP_ERROR", spawnID, fmt.Errorf("existing spawn not found for request-id %q", requestID))
				}
				Fatal("resolving idempotency conflict: existing spawn not found for request-id %q", requestID)
			}
			if jsonOutput {
				_ = json.NewEncoder(os.Stdout).Encode(spawnResult{Success: true, SpawnID: existing.SpawnID, PID: 0})
				return
			}
			fmt.Printf("%s Reusing existing remote runtime %s for request-id %s\n", term.Bold("af spawn:"), term.Cyan(existing.SpawnID), term.Cyan(requestID))
			return
		}
		if jsonOutput {
			fatalSpawnJSON("STORE_ERROR", spawnID, fmt.Errorf("persisting remote spawn request: %w", err))
		}
		Fatal("persisting remote spawn request: %v", err)
	}

	client := daemon.NewSpritesClient(token)
	created, err := client.Create(cmd.Context(), daemon.ProviderCreateRequest{
		SpawnID:   spawnID,
		RequestID: requestID,
		Project:   cfg.Project,
		Prompt:    userPrompt,
	})
	if err != nil {
		rec.State = daemon.RemoteSpawnFailed
		if isAmbiguousProviderCreateError(err) {
			rec.State = daemon.RemoteSpawnUnknown
		}
		rec.LastError = err.Error()
		_ = store.Upsert(rec)
		if jsonOutput {
			fatalSpawnJSON("PROVIDER_CREATE_ERROR", spawnID, fmt.Errorf("sprites create failed: %w", err))
		}
		Fatal("sprites create failed: %v", err)
	}

	rec.ProviderSandboxID = created.SandboxID
	rec.ProviderOperation = created.OperationID
	rec.ServerRef = created.AttachRef
	if _, err := daemon.ValidateServerURLAttachTarget(rec.ServerRef); err != nil {
		rec.State = daemon.RemoteSpawnFailed
		rec.LastError = err.Error()
		_ = store.Upsert(rec)
		if jsonOutput {
			fatalSpawnJSON("UNTRUSTED_ATTACH_TARGET", spawnID, fmt.Errorf("sprites returned untrusted attach target: %w", err))
		}
		Fatal("sprites returned untrusted attach target: %v", err)
	}
	rec.State = daemon.RemoteSpawnSpawning
	if err := store.Upsert(rec); err != nil {
		if jsonOutput {
			fatalSpawnJSON("STORE_ERROR", spawnID, fmt.Errorf("persisting remote spawn state: %w", err))
		}
		Fatal("persisting remote spawn state: %v", err)
	}

	if jsonOutput {
		_ = json.NewEncoder(os.Stdout).Encode(spawnResult{Success: true, SpawnID: spawnID, PID: 0})
		return
	}

	fmt.Printf("%s Spawned remote runtime %s on sprites\n", term.Bold("af spawn:"), term.Cyan(spawnID))
	fmt.Printf("%s session is not ready yet; use `af session attach %s` to check readiness\n", term.Dim("note:"), spawnID)
}

// isAmbiguousProviderCreateError returns true when the HTTP call outcome
// is indeterminate — the request may or may not have been processed.
// Uses Go's typed error interfaces (net.Error.Timeout, net.Error.Temporary)
// rather than string matching, so new transports or error messages don't
// silently change classification.
func isAmbiguousProviderCreateError(err error) bool {
	if err == nil {
		return false
	}
	// Context deadline / cancellation — request may have reached the server.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	// Net-level timeout or temporary error (includes TCP resets, DNS transient).
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	// Unexpected EOF during response read — server may have acted.
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return false
}

// buildAgentProc creates a configured exec.Cmd for the agent process.
// Callers set Stdout/Stdin/Stderr as needed for their execution mode.
func buildAgentProc(ctx context.Context, spawnCmd, prompt, agentID string) *exec.Cmd {
	parts := strings.Fields(spawnCmd)
	if len(parts) == 0 {
		Fatal("empty spawn command")
	}
	parts = append(parts, prompt)

	proc := exec.CommandContext(ctx, parts[0], parts[1:]...)
	proc.Env = append(os.Environ(), "AETHERFLOW_AGENT_ID="+agentID)
	proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return proc
}

// registerSpawn attempts to register the spawned agent with the daemon.
// Best-effort — if the daemon isn't running, we log a warning for non-
// connection errors and continue.
func registerSpawn(socketPath, spawnID string, pid int, prompt string) {
	c := client.New(socketPath)
	if err := c.SpawnRegister(client.SpawnRegisterParams{
		SpawnID: spawnID,
		PID:     pid,
		Prompt:  prompt,
	}); err != nil {
		// Connection refused = daemon not running — expected, silent.
		// Anything else is worth surfacing.
		if !isConnectionRefused(err) {
			fmt.Fprintf(os.Stderr, "af spawn: warning: daemon registration failed: %v\n", err)
		}
	}
}

// deregisterSpawn attempts to remove the spawned agent from the daemon registry.
// Best-effort — if the daemon isn't running, we silently continue.
func deregisterSpawn(socketPath, spawnID string) {
	c := client.New(socketPath)
	_ = c.SpawnDeregister(spawnID)
}

// isConnectionRefused returns true if the error is a "connection refused"
// from dialing a Unix socket — i.e., no daemon is running.
func isConnectionRefused(err error) bool {
	if opErr, ok := err.(*net.OpError); ok {
		if sysErr, ok := opErr.Err.(*os.SyscallError); ok {
			return sysErr.Err == syscall.ECONNREFUSED
		}
	}
	return false
}

// spawnResult is the JSON output for --json mode.
type spawnResult struct {
	Success bool   `json:"success"`
	SpawnID string `json:"spawn_id"`
	PID     int    `json:"pid"`
}

// spawnErrorResult is the JSON error output for --json mode.
type spawnErrorResult struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	SpawnID string `json:"spawn_id,omitempty"`
	Error   string `json:"error"`
}

// fatalSpawnJSON emits a structured JSON error to stdout and exits.
// Use instead of Fatal() when jsonOutput is true.
func fatalSpawnJSON(code, spawnID string, err error) {
	_ = json.NewEncoder(os.Stdout).Encode(spawnErrorResult{
		Success: false,
		Code:    code,
		SpawnID: spawnID,
		Error:   err.Error(),
	})
	os.Exit(1)
}

// runForeground launches the agent in the current terminal.
func runForeground(spawnID, userPrompt, spawnCmd, prompt, socketPath string, jsonOutput bool) {
	if !jsonOutput {
		fmt.Printf("%s Spawning agent %s\n", term.Bold("af spawn:"), term.Cyan(spawnID))
		fmt.Println()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	proc := buildAgentProc(ctx, spawnCmd, prompt, spawnID)
	proc.Stdout = os.Stdout
	proc.Stderr = os.Stderr
	proc.Stdin = os.Stdin

	// Start (not Run) so we can register with the daemon before waiting.
	if err := proc.Start(); err != nil {
		Fatal("failed to start agent: %v", err)
	}

	if jsonOutput {
		_ = json.NewEncoder(os.Stdout).Encode(spawnResult{
			Success: true,
			SpawnID: spawnID,
			PID:     proc.Process.Pid,
		})
	}

	// Register with daemon for observability (best-effort).
	registerSpawn(socketPath, spawnID, proc.Process.Pid, userPrompt)

	// Wait for the process to exit.
	waitErr := proc.Wait()

	// Deregister from daemon (best-effort).
	deregisterSpawn(socketPath, spawnID)

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		Fatal("agent process failed: %v", waitErr)
	}

	if !jsonOutput {
		fmt.Printf("\n%s Agent %s finished\n", term.Bold("af spawn:"), term.Cyan(spawnID))
	}
}

// runDetached launches the agent process in the background.
// The rendered prompt is passed directly to the spawn command, bypassing
// af spawn entirely so there's no double-rendering or flag-forwarding.
// Stdout/stderr are discarded — observability comes from the plugin event pipeline.
func runDetached(spawnID, userPrompt, spawnCmd, prompt, socketPath string, jsonOutput bool) {
	proc := buildAgentProc(context.Background(), spawnCmd, prompt, spawnID)

	// Redirect stdout/stderr to /dev/null. Observability is provided by the
	// plugin event pipeline (session events flow through the daemon's event buffer).
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		Fatal("opening /dev/null: %v", err)
	}
	proc.Stdout = devNull
	proc.Stderr = devNull

	if err := proc.Start(); err != nil {
		_ = devNull.Close()
		Fatal("failed to start agent: %v", err)
	}

	// Detach: don't wait for the child.
	_ = devNull.Close()

	// Register with daemon for observability (best-effort).
	// The daemon's sweep will clean up the entry when the PID dies.
	registerSpawn(socketPath, spawnID, proc.Process.Pid, userPrompt)

	if jsonOutput {
		_ = json.NewEncoder(os.Stdout).Encode(spawnResult{
			Success: true,
			SpawnID: spawnID,
			PID:     proc.Process.Pid,
		})
	} else {
		fmt.Printf("%s Spawned agent %s (pid %d)\n", term.Bold("af spawn:"), term.Cyan(spawnID), proc.Process.Pid)
		fmt.Printf("%s af logs %s -f\n", term.Dim("logs:"), spawnID)
	}
}
