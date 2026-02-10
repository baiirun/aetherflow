package harness

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// validateComponentName validates a skill/agent/plugin name for security.
// It prevents path traversal, enforces length limits, and restricts characters.
func validateComponentName(name string) error {
	if name == "" {
		return errors.New("name cannot be empty")
	}

	if len(name) > 255 {
		return errors.New("name too long (max 255 characters)")
	}

	// Prevent path traversal - name must not contain path separators
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("name cannot contain path separators: %q", name)
	}

	// Reject special directory names
	if name == "." || name == ".." {
		return fmt.Errorf("invalid name: %q", name)
	}

	// Use filepath.Base as additional safety check
	if filepath.Base(name) != name {
		return fmt.Errorf("name contains invalid path components: %q", name)
	}

	return nil
}

// detectHarness checks if a harness is installed by looking for the binary on PATH
// or checking if the config directory exists.
func detectHarness(binaryName, configDir string) bool {
	// Check if binary is on PATH
	if _, err := exec.LookPath(binaryName); err == nil {
		return true
	}

	// Check if config directory exists
	if info, err := os.Stat(configDir); err == nil && info.IsDir() {
		return true
	}

	return false
}

// openCodeHarness implements the Harness interface for OpenCode
type openCodeHarness struct {
	configDir string
}

// OpenCode returns a Harness for the OpenCode agent runtime.
// It panics if the user's home directory cannot be determined.
func OpenCode() Harness {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("failed to get user home directory: %v", err))
	}
	if home == "" {
		panic("user home directory is empty")
	}
	return &openCodeHarness{
		configDir: filepath.Join(home, ".config", "opencode"),
	}
}

func (h *openCodeHarness) Name() string {
	return "opencode"
}

func (h *openCodeHarness) SkillPath(name string) (string, error) {
	if err := validateComponentName(name); err != nil {
		return "", fmt.Errorf("invalid skill name: %w", err)
	}
	return filepath.Join(h.configDir, "skills", name, "SKILL.md"), nil
}

func (h *openCodeHarness) AgentPath(name string) (string, error) {
	if err := validateComponentName(name); err != nil {
		return "", fmt.Errorf("invalid agent name: %w", err)
	}
	return filepath.Join(h.configDir, "agents", name+".md"), nil
}

func (h *openCodeHarness) PluginPath(name string) (string, error) {
	if err := validateComponentName(name); err != nil {
		return "", fmt.Errorf("invalid plugin name: %w", err)
	}
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
	// Return not supported until actual registration logic is implemented
	return fmt.Errorf("plugin registration: %w", ErrNotSupported)
}

func (h *openCodeHarness) Detect() bool {
	return detectHarness("opencode", h.configDir)
}

// claudeHarness implements the Harness interface for Claude Code
type claudeHarness struct {
	configDir string
}

// Claude returns a Harness for the Claude Code agent runtime.
// It panics if the user's home directory cannot be determined.
func Claude() Harness {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("failed to get user home directory: %v", err))
	}
	if home == "" {
		panic("user home directory is empty")
	}
	return &claudeHarness{
		configDir: filepath.Join(home, ".claude"),
	}
}

func (h *claudeHarness) Name() string {
	return "claude"
}

func (h *claudeHarness) SkillPath(name string) (string, error) {
	if err := validateComponentName(name); err != nil {
		return "", fmt.Errorf("invalid skill name: %w", err)
	}
	return filepath.Join(h.configDir, "skills", name, "SKILL.md"), nil
}

func (h *claudeHarness) AgentPath(name string) (string, error) {
	if err := validateComponentName(name); err != nil {
		return "", fmt.Errorf("invalid agent name: %w", err)
	}
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
	return detectHarness("claude", h.configDir)
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
	return "", fmt.Errorf("skill installation: %w", ErrNotSupported)
}

func (h *codexHarness) AgentPath(name string) (string, error) {
	return "", fmt.Errorf("agent installation: %w", ErrNotSupported)
}

func (h *codexHarness) PluginPath(name string) (string, error) {
	return "", fmt.Errorf("plugin installation: %w", ErrNotSupported)
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
	return fmt.Errorf("plugin registration: %w", ErrNotSupported)
}

func (h *codexHarness) Detect() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	codexDir := filepath.Join(home, ".codex")
	return detectHarness("codex", codexDir)
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
