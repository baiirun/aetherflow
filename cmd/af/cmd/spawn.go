package cmd

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	f.String("log-dir", daemon.DefaultLogDir, "Directory for agent JSONL log files")
}

func runSpawn(cmd *cobra.Command, args []string) {
	userPrompt := args[0]

	detach, _ := cmd.Flags().GetBool("detach")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	solo, _ := cmd.Flags().GetBool("solo")
	spawnCmd, _ := cmd.Flags().GetString("spawn-cmd")
	promptDir, _ := cmd.Flags().GetString("prompt-dir")
	logDir, _ := cmd.Flags().GetString("log-dir")

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
	if !cmd.Flags().Changed("log-dir") && fileCfg.LogDir != "" {
		logDir = fileCfg.LogDir
	}

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

	// Set up JSONL logging directory.
	if err := os.MkdirAll(logDir, 0700); err != nil {
		Fatal("creating log directory: %v", err)
	}
	logPath := filepath.Join(logDir, spawnID+".jsonl")

	// Resolve the daemon socket for best-effort registration.
	socketPath := resolveSocketPath(cmd)

	if detach {
		runDetached(spawnID, userPrompt, spawnCmd, prompt, logPath, socketPath, jsonOutput)
		return
	}

	runForeground(spawnID, userPrompt, spawnCmd, prompt, logPath, socketPath, jsonOutput)
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
	SpawnID string `json:"spawn_id"`
	PID     int    `json:"pid"`
	LogPath string `json:"log_path"`
}

// runForeground launches the agent in the current terminal.
// Output goes to both the terminal and the log file.
func runForeground(spawnID, userPrompt, spawnCmd, prompt, logPath, socketPath string, jsonOutput bool) {
	if !jsonOutput {
		fmt.Printf("%s Spawning agent %s\n", term.Bold("af spawn:"), term.Cyan(spawnID))
		fmt.Printf("%s %s\n", term.Dim("log:"), logPath)
		fmt.Println()
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		Fatal("opening log file: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	proc := buildAgentProc(ctx, spawnCmd, prompt, spawnID)

	// Tee stdout to both terminal and log file.
	proc.Stdout = io.MultiWriter(os.Stdout, logFile)
	proc.Stderr = os.Stderr
	proc.Stdin = os.Stdin

	// Start (not Run) so we can register with the daemon before waiting.
	if err := proc.Start(); err != nil {
		_ = logFile.Close()
		Fatal("failed to start agent: %v", err)
	}

	if jsonOutput {
		_ = json.NewEncoder(os.Stdout).Encode(spawnResult{
			SpawnID: spawnID,
			PID:     proc.Process.Pid,
			LogPath: logPath,
		})
	}

	// Register with daemon for observability (best-effort).
	registerSpawn(socketPath, spawnID, proc.Process.Pid, userPrompt)

	// Wait for the process to exit.
	waitErr := proc.Wait()
	_ = logFile.Close()

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
func runDetached(spawnID, userPrompt, spawnCmd, prompt, logPath, socketPath string, jsonOutput bool) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		Fatal("opening log file for detached process: %v", err)
	}

	proc := buildAgentProc(context.Background(), spawnCmd, prompt, spawnID)
	proc.Stdout = logFile
	proc.Stderr = logFile

	if err := proc.Start(); err != nil {
		_ = logFile.Close()
		Fatal("failed to start agent: %v", err)
	}

	// Detach: don't wait for the child.
	_ = logFile.Close()

	// Register with daemon for observability (best-effort).
	// The daemon's sweep will clean up the entry when the PID dies.
	registerSpawn(socketPath, spawnID, proc.Process.Pid, userPrompt)

	if jsonOutput {
		_ = json.NewEncoder(os.Stdout).Encode(spawnResult{
			SpawnID: spawnID,
			PID:     proc.Process.Pid,
			LogPath: logPath,
		})
	} else {
		fmt.Printf("%s Spawned agent %s (pid %d)\n", term.Bold("af spawn:"), term.Cyan(spawnID), proc.Process.Pid)
		fmt.Printf("%s %s\n", term.Dim("log:"), logPath)
		fmt.Printf("%s tail -f %s\n", term.Dim("tail:"), logPath)
	}
}
