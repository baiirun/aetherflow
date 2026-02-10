package harness

import (
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

func TestCodexHarness_NotSupported(t *testing.T) {
	h := Codex()

	_, err := h.SkillPath("review-auto")
	if err == nil {
		t.Error("Codex should return error for skills")
	}

	_, err = h.AgentPath("code-reviewer")
	if err == nil {
		t.Error("Codex should return error for agents")
	}

	_, err = h.PluginPath("activity-logger")
	if err == nil {
		t.Error("Codex should return error for plugins")
	}
}

func TestCodexHarness_Supports(t *testing.T) {
	h := Codex()

	if h.SupportsSkills() {
		t.Error("Codex should not support skills")
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

	// RegisterPlugin should be a no-op or return nil for opencode
	// The actual registration logic will be tested separately
	err := h.RegisterPlugin("activity-logger", "/path/to/plugin.ts")
	if err != nil {
		t.Errorf("RegisterPlugin() error = %v", err)
	}
}

func TestClaudeHarness_RegisterPlugin(t *testing.T) {
	h := Claude()

	err := h.RegisterPlugin("activity-logger", "/path/to/plugin.ts")
	if err == nil {
		t.Error("Claude RegisterPlugin should return error (not supported)")
	}
}
