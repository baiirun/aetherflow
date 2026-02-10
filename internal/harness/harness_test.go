package harness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeHarness_SkillPath(t *testing.T) {
	h := OpenCode()

	skillPath, err := h.SkillPath("review-auto")
	if err != nil {
		t.Fatalf("SkillPath() error = %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "opencode", "skills", "review-auto", "SKILL.md")
	if skillPath != expected {
		t.Errorf("SkillPath() = %v, want %v", skillPath, expected)
	}
}

func TestOpenCodeHarness_AgentPath(t *testing.T) {
	h := OpenCode()

	agentPath, err := h.AgentPath("code-reviewer")
	if err != nil {
		t.Fatalf("AgentPath() error = %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "opencode", "agents", "code-reviewer.md")
	if agentPath != expected {
		t.Errorf("AgentPath() = %v, want %v", agentPath, expected)
	}
}

func TestOpenCodeHarness_PluginPath(t *testing.T) {
	h := OpenCode()

	pluginPath, err := h.PluginPath("activity-logger")
	if err != nil {
		t.Fatalf("PluginPath() error = %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "opencode", "plugins", "activity-logger.ts")
	if pluginPath != expected {
		t.Errorf("PluginPath() = %v, want %v", pluginPath, expected)
	}
}

func TestOpenCodeHarness_Supports(t *testing.T) {
	h := OpenCode()

	if !h.SupportsSkills() {
		t.Error("OpenCode should support skills")
	}
	if !h.SupportsAgents() {
		t.Error("OpenCode should support agents")
	}
	if !h.SupportsPlugins() {
		t.Error("OpenCode should support plugins")
	}
}

func TestOpenCodeHarness_Detect(t *testing.T) {
	h := OpenCode()

	// Detection should check if opencode binary exists on PATH or ~/.config/opencode/ directory exists
	// We can't easily test PATH check in unit tests, but we can verify the method exists
	detected := h.Detect()

	// This test will vary based on local setup, so we just verify it doesn't panic
	_ = detected
}

func TestClaudeHarness_SkillPath(t *testing.T) {
	h := Claude()

	skillPath, err := h.SkillPath("review-auto")
	if err != nil {
		t.Fatalf("SkillPath() error = %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".claude", "skills", "review-auto", "SKILL.md")
	if skillPath != expected {
		t.Errorf("SkillPath() = %v, want %v", skillPath, expected)
	}
}

func TestClaudeHarness_AgentPath(t *testing.T) {
	h := Claude()

	agentPath, err := h.AgentPath("code-reviewer")
	if err != nil {
		t.Fatalf("AgentPath() error = %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".claude", "agents", "code-reviewer.md")
	if agentPath != expected {
		t.Errorf("AgentPath() = %v, want %v", agentPath, expected)
	}
}

func TestClaudeHarness_PluginPath(t *testing.T) {
	h := Claude()

	_, err := h.PluginPath("activity-logger")
	if err == nil {
		t.Error("Claude should not support plugins yet")
	}
}

func TestClaudeHarness_Supports(t *testing.T) {
	h := Claude()

	if !h.SupportsSkills() {
		t.Error("Claude should support skills")
	}
	if !h.SupportsAgents() {
		t.Error("Claude should support agents")
	}
	if h.SupportsPlugins() {
		t.Error("Claude should not support plugins yet")
	}
}

func TestCodexHarness_SkillPath(t *testing.T) {
	h := Codex()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	path, err := h.SkillPath("review-auto")
	if err != nil {
		t.Fatalf("SkillPath() unexpected error: %v", err)
	}

	want := filepath.Join(home, ".agents", "skills", "review-auto", "SKILL.md")
	if path != want {
		t.Errorf("SkillPath() = %q, want %q", path, want)
	}
}

func TestCodexHarness_NotSupported(t *testing.T) {
	h := Codex()

	// Skills ARE supported by Codex
	_, err := h.SkillPath("review-auto")
	if err != nil {
		t.Errorf("Codex should support skills, got error: %v", err)
	}

	// Agents are NOT supported
	_, err = h.AgentPath("code-reviewer")
	if err == nil {
		t.Error("Codex should return error for agents")
	}
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("AgentPath() error should wrap ErrNotSupported, got %v", err)
	}

	// Plugins are NOT supported
	_, err = h.PluginPath("activity-logger")
	if err == nil {
		t.Error("Codex should return error for plugins")
	}
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("PluginPath() error should wrap ErrNotSupported, got %v", err)
	}
}

func TestCodexHarness_Supports(t *testing.T) {
	h := Codex()

	if !h.SupportsSkills() {
		t.Error("Codex should support skills")
	}
	if h.SupportsAgents() {
		t.Error("Codex should not support agents")
	}
	if h.SupportsPlugins() {
		t.Error("Codex should not support plugins")
	}
}

func TestAllHarnesses_Names(t *testing.T) {
	tests := []struct {
		harness Harness
		want    string
	}{
		{OpenCode(), "opencode"},
		{Claude(), "claude"},
		{Codex(), "codex"},
	}

	for _, tt := range tests {
		if got := tt.harness.Name(); got != tt.want {
			t.Errorf("%s.Name() = %v, want %v", tt.want, got, tt.want)
		}
	}
}

func TestOpenCodeHarness_RegisterPlugin(t *testing.T) {
	h := OpenCode()

	// RegisterPlugin should return ErrNotSupported until implemented
	err := h.RegisterPlugin("activity-logger", "/path/to/plugin.ts")
	if err == nil {
		t.Error("RegisterPlugin() should return error (not yet implemented)")
	}
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("RegisterPlugin() error should wrap ErrNotSupported, got %v", err)
	}
}

func TestClaudeHarness_RegisterPlugin(t *testing.T) {
	h := Claude()

	err := h.RegisterPlugin("activity-logger", "/path/to/plugin.ts")
	if err == nil {
		t.Error("Claude RegisterPlugin should return error (not supported)")
	}
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("RegisterPlugin() error should wrap ErrNotSupported, got %v", err)
	}
}

func TestValidateComponentName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid name", "review-auto", false},
		{"valid with underscore", "code_reviewer", false},
		{"valid with numbers", "test123", false},
		{"empty string", "", true},
		{"path traversal up", "../etc/passwd", true},
		{"path traversal down", "foo/bar", true},
		{"absolute path", "/etc/passwd", true},
		{"windows path", "foo\\bar", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"too long", string(make([]byte, 256)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateComponentName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateComponentName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestOpenCodeHarness_SkillPath_InvalidName(t *testing.T) {
	h := OpenCode()

	tests := []string{"", "../etc/passwd", "foo/bar", "."}
	for _, name := range tests {
		_, err := h.SkillPath(name)
		if err == nil {
			t.Errorf("SkillPath(%q) should return error for invalid name", name)
		}
	}
}

func TestOpenCodeHarness_AgentPath_InvalidName(t *testing.T) {
	h := OpenCode()

	tests := []string{"", "../etc/passwd", "foo/bar", "."}
	for _, name := range tests {
		_, err := h.AgentPath(name)
		if err == nil {
			t.Errorf("AgentPath(%q) should return error for invalid name", name)
		}
	}
}

func TestOpenCodeHarness_PluginPath_InvalidName(t *testing.T) {
	h := OpenCode()

	tests := []string{"", "../etc/passwd", "foo/bar", "."}
	for _, name := range tests {
		_, err := h.PluginPath(name)
		if err == nil {
			t.Errorf("PluginPath(%q) should return error for invalid name", name)
		}
	}
}

func TestCodexHarness_ErrorWrapping(t *testing.T) {
	h := Codex()

	// SkillPath should succeed, not return ErrNotSupported
	_, err := h.SkillPath("test")
	if err != nil {
		t.Errorf("SkillPath() should succeed for valid name, got %v", err)
	}

	// AgentPath should return ErrNotSupported
	_, err = h.AgentPath("test")
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("AgentPath() error should wrap ErrNotSupported, got %v", err)
	}

	// PluginPath should return ErrNotSupported
	_, err = h.PluginPath("test")
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("PluginPath() error should wrap ErrNotSupported, got %v", err)
	}

	// RegisterPlugin should return ErrNotSupported
	err = h.RegisterPlugin("test", "/path")
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("RegisterPlugin() error should wrap ErrNotSupported, got %v", err)
	}
}

func TestAll(t *testing.T) {
	harnesses := All()
	if len(harnesses) != 3 {
		t.Errorf("All() returned %d harnesses, want 3", len(harnesses))
	}

	names := make(map[string]bool)
	for _, h := range harnesses {
		if h == nil {
			t.Fatal("All() returned nil harness")
		}
		names[h.Name()] = true
	}

	want := []string{"opencode", "claude", "codex"}
	for _, name := range want {
		if !names[name] {
			t.Errorf("All() missing harness: %s", name)
		}
	}
}

func TestDetected(t *testing.T) {
	// Detected() depends on system state, so we just verify it doesn't panic
	// and returns a valid slice
	detected := Detected()
	if detected == nil {
		t.Error("Detected() returned nil")
	}

	// Verify no nil harnesses in the result
	for i, h := range detected {
		if h == nil {
			t.Errorf("Detected() returned nil harness at index %d", i)
		}
	}
}
