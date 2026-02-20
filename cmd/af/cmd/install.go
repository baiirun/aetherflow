package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/baiirun/aetherflow/internal/install"
	"github.com/baiirun/aetherflow/internal/term"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install aetherflow skills, agents, and plugins to opencode",
	Long: `Install bundled skill, agent, and plugin definitions to your opencode
configuration directory (~/.config/opencode/ by default).

Skills and agents are required for aetherflow's worker agents to perform
code reviews and knowledge compounding. The aetherflow-events plugin
enables the event pipeline that streams opencode session events to the
daemon for real-time observability.

Running this command ensures your opencode installation has the
definitions and plugins it needs.

The command is idempotent — files that are already up to date are skipped.`,
	Run: runInstall,
}

func runInstall(cmd *cobra.Command, args []string) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	target, _ := cmd.Flags().GetString("target")
	asJSON, _ := cmd.Flags().GetBool("json")
	check, _ := cmd.Flags().GetBool("check")

	// --json implies --yes (agents requesting JSON are inherently non-interactive).
	if asJSON {
		yes = true
	}

	// Resolve target directory.
	targetDir, err := resolveInstallTarget(target)
	if err != nil {
		Fatal("%v", err)
	}

	// Detect opencode if --target was not explicitly set.
	if !cmd.Flags().Changed("target") {
		if err := detectOpencode(targetDir); err != nil {
			Fatal("%v", err)
		}
	}

	// Plan once — carries source bytes for Execute.
	actions, err := install.Plan(targetDir)
	if err != nil {
		Fatal("%v", err)
	}

	// Count planned actions.
	var writeCount, skipCount int
	for _, a := range actions {
		switch a.Action {
		case install.ActionWrite:
			writeCount++
		case install.ActionSkip:
			skipCount++
		default:
			Fatal("install: unknown action %s for %s", a.Action, a.RelPath)
		}
	}

	// --check mode: exit 0 if up-to-date, exit 1 if install needed.
	if check {
		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(installCheckJSON{
				Target:   targetDir,
				UpToDate: writeCount == 0,
				ToWrite:  writeCount,
				ToSkip:   skipCount,
				Executed: false,
			}); err != nil {
				Fatal("failed to encode JSON: %v", err)
			}
		}
		if writeCount > 0 {
			if !asJSON {
				fmt.Printf("%d files need updating.\n", writeCount)
			}
			os.Exit(1)
		}
		if !asJSON {
			fmt.Println("Everything is up to date.")
		}
		return
	}

	// Nothing to do.
	if writeCount == 0 {
		if asJSON {
			emitInstallJSON(targetDir, actions, nil, false)
		} else {
			fmt.Println("Everything is up to date.")
		}
		return
	}

	// Dry-run or interactive display.
	if !asJSON {
		fmt.Printf("Installing aetherflow skills, agents, and plugins to %s:\n\n", targetDir)
		for _, a := range actions {
			switch a.Action {
			case install.ActionWrite:
				fmt.Printf("  %s  %s\n", term.Green("write"), a.RelPath)
			case install.ActionSkip:
				fmt.Printf("  %s   %s %s\n", term.Dim("skip"), a.RelPath, term.Dim("(up to date)"))
			}
		}
		fmt.Println()
	}

	// Dry-run: stop here.
	if dryRun {
		if asJSON {
			emitInstallJSON(targetDir, actions, nil, false)
		} else {
			fmt.Printf("%d to write, %d up to date. (dry run — no files written)\n", writeCount, skipCount)
		}
		return
	}

	// Confirm unless --yes.
	if !yes {
		fmt.Print("Proceed? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		input, readErr := reader.ReadString('\n')
		if readErr != nil {
			fmt.Println("Aborted.")
			return
		}
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "" && input != "y" && input != "yes" {
			fmt.Println("Aborted.")
			return
		}
	}

	// Execute using the pre-computed plan — single pass, no re-reading.
	result := install.Execute(actions)

	if asJSON {
		emitInstallJSON(targetDir, actions, result, true)
	} else {
		// Report results.
		fmt.Println()
		for _, a := range actions {
			switch {
			case a.Err != nil:
				fmt.Printf("  %s  %s — %v\n", term.Red("✗"), a.RelPath, a.Err)
			case a.Action == install.ActionSkip:
				fmt.Printf("  %s  %s %s\n", term.Dim("·"), a.RelPath, term.Dim("(up to date)"))
			default:
				fmt.Printf("  %s  %s\n", term.Green("✓"), a.RelPath)
			}
		}
		fmt.Println()

		fmt.Printf("Done. %d written, %d up to date.", result.Written, result.Skipped)
		if result.Errors > 0 {
			fmt.Printf(" %s", term.Redf("%d failed.", result.Errors))
		}
		fmt.Println()
	}

	if result.Errors > 0 {
		os.Exit(1)
	}
}

// resolveInstallTarget returns the absolute path for the install target.
// If target is empty, it defaults to $XDG_CONFIG_HOME/opencode/ (falling
// back to ~/.config/opencode/).
func resolveInstallTarget(target string) (string, error) {
	if target != "" {
		return filepath.Abs(target)
	}

	// Respect XDG_CONFIG_HOME if set and absolute (per XDG spec).
	// Ignore relative values — they violate the spec.
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" || !filepath.IsAbs(configDir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not determine home directory: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}

	return filepath.Join(configDir, "opencode"), nil
}

// detectOpencode checks that opencode is installed on this machine.
// Two signals — either is sufficient:
//  1. "opencode" binary is on $PATH
//  2. The target config directory exists
//
// This intentionally uses direct OS calls since it's CLI-layer code.
func detectOpencode(targetDir string) error {
	if _, err := exec.LookPath("opencode"); err == nil {
		slog.Debug("opencode found in PATH")
		return nil
	}

	info, err := os.Stat(targetDir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("opencode: %s exists but is not a directory", targetDir)
		}
		slog.Debug("opencode config dir exists", "dir", targetDir)
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("opencode: could not stat %s: %w", targetDir, err)
	}

	slog.Debug("opencode not detected", "targetDir", targetDir)
	return fmt.Errorf(`opencode not detected. Install opencode first:

  brew install opencode    # macOS
  curl -fsSL https://opencode.ai/install | bash  # linux

  https://opencode.ai

Or use --target to specify a custom install location`)
}

// --- JSON output types ---

type installCheckJSON struct {
	Target   string `json:"target"`
	UpToDate bool   `json:"up_to_date"`
	ToWrite  int    `json:"to_write"`
	ToSkip   int    `json:"to_skip"`
	Executed bool   `json:"executed"`
}

type installActionJSON struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Error  string `json:"error,omitempty"`
}

type installResultJSON struct {
	Target   string              `json:"target"`
	Actions  []installActionJSON `json:"actions"`
	Summary  installSummaryJSON  `json:"summary"`
	Executed bool                `json:"executed"`
}

type installSummaryJSON struct {
	Written int `json:"written"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

func emitInstallJSON(targetDir string, actions []install.FileAction, result *install.Result, executed bool) {
	out := installResultJSON{
		Target:   targetDir,
		Executed: executed,
	}

	for _, a := range actions {
		aj := installActionJSON{
			Path:   a.RelPath,
			Action: a.Action.String(),
		}
		if a.Err != nil {
			aj.Error = a.Err.Error()
		}
		out.Actions = append(out.Actions, aj)
	}

	if result != nil {
		out.Summary = installSummaryJSON{
			Written: result.Written,
			Skipped: result.Skipped,
			Errors:  result.Errors,
		}
	} else {
		// Plan-only: derive summary from actions.
		for _, a := range actions {
			switch a.Action {
			case install.ActionWrite:
				out.Summary.Written++
			case install.ActionSkip:
				out.Summary.Skipped++
			}
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	// Best-effort: stdout write failures are unrecoverable in a CLI.
	_ = enc.Encode(out)
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().Bool("dry-run", false, "Show what would be installed without writing files")
	installCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	installCmd.Flags().String("target", "", "Override install directory (default: ~/.config/opencode/)")
	installCmd.Flags().Bool("json", false, "Output structured JSON (implies --yes)")
	installCmd.Flags().Bool("check", false, "Check if install is needed (exit 0=up-to-date, 1=needs install)")
}
