package harness

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Harness defines the interface for agent harness installations.
// Each harness (opencode, claude, codex) stores skills, agents, and plugins
// in different filesystem locations with potentially different formats.
type Harness interface {
	// Name returns the harness identifier (opencode, claude, codex)
	Name() string

	// SkillPath returns the full path where a skill should be installed
	SkillPath(name string) (string, error)

	// AgentPath returns the full path where an agent definition should be installed
	AgentPath(name string) (string, error)

	// PluginPath returns the full path where a plugin should be installed
	PluginPath(name string) (string, error)

	// SupportsSkills returns true if this harness supports skill installation
	SupportsSkills() bool

	// SupportsAgents returns true if this harness supports agent installation
	SupportsAgents() bool

	// SupportsPlugins returns true if this harness supports plugin installation
	SupportsPlugins() bool

	// RegisterPlugin handles harness-specific plugin registration (e.g., updating config files)
	// Returns an error if the harness doesn't support plugins or registration fails
	RegisterPlugin(name, path string) error

	// Detect returns true if this harness is installed on the system
	Detect() bool
}

var (
	ErrNotSupported = errors.New("not supported by this harness")
	ErrNotInstalled = errors.New("harness not detected on this system")
)

// openCodeHarness implements the Harness interface for OpenCode
type openCodeHarness struct {
	configDir string
}

// OpenCode returns a Harness for the OpenCode agent runtime
func OpenCode() Harness {
	home, _ := os.UserHomeDir()
	return &openCodeHarness{
		configDir: filepath.Join(home, ".config", "opencode"),
	}
}

func (h *openCodeHarness) Name() string {
	return "opencode"
}

func (h *openCodeHarness) SkillPath(name string) (string, error) {
	return filepath.Join(h.configDir, "skills", name, "SKILL.md"), nil
}

func (h *openCodeHarness) AgentPath(name string) (string, error) {
	return filepath.Join(h.configDir, "agents", name+".md"), nil
}

func (h *openCodeHarness) PluginPath(name string) (string, error) {
	return filepath.Join(h.configDir, "plugins", name+".ts"), nil
}

func (h *openCodeHarness) SupportsSkills() bool {
	return true
}

func (h *openCodeHarness) SupportsAgents() bool {
	return true
}

func (h *openCodeHarness) SupportsPlugins() bool {
	return true
}

func (h *openCodeHarness) RegisterPlugin(name, path string) error {
	// OpenCode plugins may need registration in opencode.json
	// For now, this is a no-op - actual registration logic will be implemented
	// when the install command is built
	return nil
}

func (h *openCodeHarness) Detect() bool {
	// Check if opencode binary is on PATH
	if _, err := exec.LookPath("opencode"); err == nil {
		return true
	}

	// Check if config directory exists
	if info, err := os.Stat(h.configDir); err == nil && info.IsDir() {
		return true
	}

	return false
}

// claudeHarness implements the Harness interface for Claude Code
type claudeHarness struct {
	configDir string
}

// Claude returns a Harness for the Claude Code agent runtime
func Claude() Harness {
	home, _ := os.UserHomeDir()
	return &claudeHarness{
		configDir: filepath.Join(home, ".claude"),
	}
}

func (h *claudeHarness) Name() string {
	return "claude"
}

func (h *claudeHarness) SkillPath(name string) (string, error) {
	return filepath.Join(h.configDir, "skills", name, "SKILL.md"), nil
}

func (h *claudeHarness) AgentPath(name string) (string, error) {
	return filepath.Join(h.configDir, "agents", name+".md"), nil
}

func (h *claudeHarness) PluginPath(name string) (string, error) {
	return "", fmt.Errorf("plugin installation: %w", ErrNotSupported)
}

func (h *claudeHarness) SupportsSkills() bool {
	return true
}

func (h *claudeHarness) SupportsAgents() bool {
	return true
}

func (h *claudeHarness) SupportsPlugins() bool {
	return false
}

func (h *claudeHarness) RegisterPlugin(name, path string) error {
	return fmt.Errorf("plugin registration: %w", ErrNotSupported)
}

func (h *claudeHarness) Detect() bool {
	// Check if claude binary is on PATH
	if _, err := exec.LookPath("claude"); err == nil {
		return true
	}

	// Check if config directory exists
	if info, err := os.Stat(h.configDir); err == nil && info.IsDir() {
		return true
	}

	return false
}

// codexHarness implements the Harness interface for Codex
type codexHarness struct{}

// Codex returns a Harness for the Codex agent runtime
func Codex() Harness {
	return &codexHarness{}
}

func (h *codexHarness) Name() string {
	return "codex"
}

func (h *codexHarness) SkillPath(name string) (string, error) {
	return "", fmt.Errorf("codex harness is not yet supported - skill installation locations need investigation")
}

func (h *codexHarness) AgentPath(name string) (string, error) {
	return "", fmt.Errorf("codex harness is not yet supported - agent installation locations need investigation")
}

func (h *codexHarness) PluginPath(name string) (string, error) {
	return "", fmt.Errorf("codex harness is not yet supported - plugin installation locations need investigation")
}

func (h *codexHarness) SupportsSkills() bool {
	return false
}

func (h *codexHarness) SupportsAgents() bool {
	return false
}

func (h *codexHarness) SupportsPlugins() bool {
	return false
}

func (h *codexHarness) RegisterPlugin(name, path string) error {
	return fmt.Errorf("codex harness is not yet supported")
}

func (h *codexHarness) Detect() bool {
	// Check if codex binary is on PATH
	if _, err := exec.LookPath("codex"); err == nil {
		return true
	}

	home, _ := os.UserHomeDir()
	codexDir := filepath.Join(home, ".codex")
	if info, err := os.Stat(codexDir); err == nil && info.IsDir() {
		return true
	}

	return false
}

// All returns all available harnesses
func All() []Harness {
	return []Harness{
		OpenCode(),
		Claude(),
		Codex(),
	}
}

// Detected returns all harnesses that are detected on the system
func Detected() []Harness {
	var detected []Harness
	for _, h := range All() {
		if h.Detect() {
			detected = append(detected, h)
		}
	}
	return detected
}
