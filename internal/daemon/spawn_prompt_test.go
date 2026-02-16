package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Embedded spawn prompt tests (promptDir == "") ---

func TestRenderSpawnPromptEmbedded(t *testing.T) {
	got, err := RenderSpawnPrompt("", "refactor auth to use JWT", "spawn-ghost_wolf", false)
	if err != nil {
		t.Fatalf("RenderSpawnPrompt (embedded) returned error: %v", err)
	}

	if !strings.Contains(got, "refactor auth to use JWT") {
		t.Error("rendered prompt should contain the user prompt")
	}
	if !strings.Contains(got, "spawn-ghost_wolf") {
		t.Error("rendered prompt should contain the spawn ID")
	}
	if strings.Contains(got, "{{user_prompt}}") {
		t.Error("rendered prompt should not contain unreplaced {{user_prompt}}")
	}
	if strings.Contains(got, "{{spawn_id}}") {
		t.Error("rendered prompt should not contain unreplaced {{spawn_id}}")
	}
	if strings.Contains(got, "{{land_steps}}") {
		t.Error("rendered prompt should not contain unreplaced {{land_steps}}")
	}
	if strings.Contains(got, "{{land_donts}}") {
		t.Error("rendered prompt should not contain unreplaced {{land_donts}}")
	}
}

func TestRenderSpawnPromptNoProgReferences(t *testing.T) {
	got, err := RenderSpawnPrompt("", "add tests", "spawn-neon_fox", false)
	if err != nil {
		t.Fatalf("RenderSpawnPrompt returned error: %v", err)
	}

	// Spawn prompts should not reference prog commands for task management.
	if strings.Contains(got, "prog show") {
		t.Error("spawn prompt should not reference prog show")
	}
	if strings.Contains(got, "prog start") {
		t.Error("spawn prompt should not reference prog start")
	}
	if strings.Contains(got, "prog review") {
		t.Error("spawn prompt should not reference prog review")
	}
	if strings.Contains(got, "prog done") {
		t.Error("spawn prompt should not reference prog done")
	}
	if strings.Contains(got, "prog block") {
		t.Error("spawn prompt should not reference prog block")
	}
}

// --- Solo vs Normal mode ---

func TestRenderSpawnPromptNormalMode(t *testing.T) {
	got, err := RenderSpawnPrompt("", "add feature X", "spawn-alpha_hawk", false)
	if err != nil {
		t.Fatalf("RenderSpawnPrompt returned error: %v", err)
	}

	// Normal mode should include PR creation.
	if !strings.Contains(got, "Create PR") {
		t.Error("normal mode should mention creating a PR")
	}
	if !strings.Contains(got, "git push") {
		t.Error("normal mode should include git push")
	}
	// Should reference spawn ID in worktree paths.
	if !strings.Contains(got, ".aetherflow/worktrees/spawn-alpha_hawk") {
		t.Error("normal mode should reference the spawn ID in worktree path")
	}
	// Should NOT include merge-to-main.
	if strings.Contains(got, "Merge to main") {
		t.Error("normal mode should NOT mention merging to main")
	}
}

func TestRenderSpawnPromptSoloMode(t *testing.T) {
	got, err := RenderSpawnPrompt("", "fix bug Y", "spawn-dark_viper", true)
	if err != nil {
		t.Fatalf("RenderSpawnPrompt returned error: %v", err)
	}

	// Solo mode should include merge-to-main.
	if !strings.Contains(got, "Merge to main") {
		t.Error("solo mode should mention merging to main")
	}
	if !strings.Contains(got, "git merge af/spawn-dark_viper") {
		t.Error("solo mode should include the merge command with spawn ID")
	}
	// Solo mode should NOT include PR creation.
	if strings.Contains(got, "Create PR") {
		t.Error("solo mode should NOT mention creating a PR")
	}
}

// --- Worktree and branch references ---

func TestRenderSpawnPromptWorktreeSetup(t *testing.T) {
	got, err := RenderSpawnPrompt("", "implement feature", "spawn-cyber_node", false)
	if err != nil {
		t.Fatalf("RenderSpawnPrompt returned error: %v", err)
	}

	// Should instruct the agent to create worktree and branch with spawn ID.
	if !strings.Contains(got, "git worktree add .aetherflow/worktrees/spawn-cyber_node -b af/spawn-cyber_node") {
		t.Error("should include worktree add command with spawn ID")
	}
}

// --- Filesystem override tests (promptDir != "") ---

func TestRenderSpawnPromptFilesystemOverride(t *testing.T) {
	dir := t.TempDir()

	content := "# Spawn\n\nObjective: {{user_prompt}}\nID: {{spawn_id}}\n"
	if err := os.WriteFile(filepath.Join(dir, "spawn.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := RenderSpawnPrompt(dir, "do the thing", "spawn-test_id", false)
	if err != nil {
		t.Fatalf("RenderSpawnPrompt returned error: %v", err)
	}

	want := "# Spawn\n\nObjective: do the thing\nID: spawn-test_id\n"
	if got != want {
		t.Errorf("RenderSpawnPrompt mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderSpawnPromptFilesystemMissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := RenderSpawnPrompt(dir, "prompt", "spawn-id", false)
	if err == nil {
		t.Fatal("expected error for missing spawn.md, got nil")
	}
	if !strings.Contains(err.Error(), "spawn.md") {
		t.Errorf("error should mention the filename, got: %v", err)
	}
}

func TestRenderSpawnPromptRejectsTemplateMarkersInPrompt(t *testing.T) {
	_, err := RenderSpawnPrompt("", "fix the {{config}} parser", "spawn-id", false)
	if err == nil {
		t.Fatal("expected error for user prompt containing '{{', got nil")
	}
	if !strings.Contains(err.Error(), "must not contain '{{'") {
		t.Errorf("error should mention template syntax conflict, got: %v", err)
	}
}

func TestRenderSpawnPromptUnresolvedVariable(t *testing.T) {
	dir := t.TempDir()

	// Template with typo: "{{ user_prompt }}" with spaces won't be replaced.
	content := "# Spawn\n\nObjective: {{ user_prompt }}\n"
	if err := os.WriteFile(filepath.Join(dir, "spawn.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := RenderSpawnPrompt(dir, "prompt", "spawn-id", false)
	if err == nil {
		t.Fatal("expected error for unresolved template variable, got nil")
	}
	if !strings.Contains(err.Error(), "unresolved template variable") {
		t.Errorf("error should mention unresolved variable, got: %v", err)
	}
}
