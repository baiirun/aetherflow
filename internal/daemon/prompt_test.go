package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Embedded prompt tests (promptDir == "") ---

func TestRenderPromptEmbedded(t *testing.T) {
	got, err := RenderPrompt("", RoleWorker, "ts-abc123", false)
	if err != nil {
		t.Fatalf("RenderPrompt (embedded) returned error: %v", err)
	}

	// The embedded worker.md contains {{task_id}} which should be replaced.
	if !strings.Contains(got, "ts-abc123") {
		t.Error("rendered prompt should contain the task ID")
	}
	if strings.Contains(got, "{{task_id}}") {
		t.Error("rendered prompt should not contain unreplaced {{task_id}}")
	}
	if strings.Contains(got, "{{land_steps}}") {
		t.Error("rendered prompt should not contain unreplaced {{land_steps}}")
	}
	if strings.Contains(got, "{{land_donts}}") {
		t.Error("rendered prompt should not contain unreplaced {{land_donts}}")
	}
}

func TestRenderPromptEmbeddedPlanner(t *testing.T) {
	got, err := RenderPrompt("", RolePlanner, "ts-plan42", false)
	if err != nil {
		t.Fatalf("RenderPrompt (embedded planner) returned error: %v", err)
	}

	if !strings.Contains(got, "ts-plan42") {
		t.Error("rendered planner prompt should contain the task ID")
	}
	if strings.Contains(got, "{{task_id}}") {
		t.Error("rendered planner prompt should not contain unreplaced {{task_id}}")
	}
}

func TestRenderPromptEmbeddedUnknownRole(t *testing.T) {
	_, err := RenderPrompt("", Role("hacker"), "ts-abc123", false)
	if err == nil {
		t.Fatal("expected error for unknown role, got nil")
	}
	if !strings.Contains(err.Error(), "unknown role") {
		t.Errorf("error should mention unknown role, got: %v", err)
	}
}

// --- Solo vs Normal mode ---

func TestRenderPromptNormalMode(t *testing.T) {
	got, err := RenderPrompt("", RoleWorker, "ts-abc123", false)
	if err != nil {
		t.Fatalf("RenderPrompt returned error: %v", err)
	}

	// Normal mode should include PR creation and prog review.
	if !strings.Contains(got, "Create PR") {
		t.Error("normal mode should mention creating a PR")
	}
	if !strings.Contains(got, "prog review ts-abc123") {
		t.Error("normal mode should use prog review")
	}
	if strings.Contains(got, "Merge to main") {
		t.Error("normal mode should NOT mention merging to main")
	}
}

func TestRenderPromptSoloMode(t *testing.T) {
	got, err := RenderPrompt("", RoleWorker, "ts-abc123", true)
	if err != nil {
		t.Fatalf("RenderPrompt returned error: %v", err)
	}

	// Solo mode should include merge-to-main and prog done.
	if !strings.Contains(got, "Merge to main") {
		t.Error("solo mode should mention merging to main")
	}
	if !strings.Contains(got, "prog done ts-abc123") {
		t.Error("solo mode should use prog done")
	}
	if !strings.Contains(got, "git merge af/ts-abc123") {
		t.Error("solo mode should include the merge command with task ID")
	}
	// Solo mode should NOT include PR creation or prog review.
	if strings.Contains(got, "Create PR") {
		t.Error("solo mode should NOT mention creating a PR")
	}
	if strings.Contains(got, "prog review") {
		t.Error("solo mode should NOT mention prog review")
	}
}

// --- Filesystem override tests (promptDir != "") ---

func TestRenderPromptFilesystemOverride(t *testing.T) {
	dir := t.TempDir()

	content := "# Worker\n\nTask: {{task_id}}\n\nRun: prog show {{task_id}}\n"
	if err := os.WriteFile(filepath.Join(dir, "worker.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := RenderPrompt(dir, RoleWorker, "ts-abc123", false)
	if err != nil {
		t.Fatalf("RenderPrompt returned error: %v", err)
	}

	want := "# Worker\n\nTask: ts-abc123\n\nRun: prog show ts-abc123\n"
	if got != want {
		t.Errorf("RenderPrompt mismatch\ngot:  %q\nwant: %q", got, want)
	}

	if strings.Contains(got, "{{task_id}}") {
		t.Error("rendered prompt still contains {{task_id}}")
	}
}

func TestRenderPromptFilesystemMissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := RenderPrompt(dir, RoleWorker, "ts-abc123", false)
	if err == nil {
		t.Fatal("expected error for missing prompt file, got nil")
	}
	if !strings.Contains(err.Error(), "worker.md") {
		t.Errorf("error should mention the filename, got: %v", err)
	}
}

func TestRenderPromptFilesystemUnresolvedVariable(t *testing.T) {
	dir := t.TempDir()

	// Template with a typo: "{{ task_id }}" (spaces) won't be replaced.
	content := "# Worker\n\nTask: {{ task_id }}\n"
	if err := os.WriteFile(filepath.Join(dir, "worker.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := RenderPrompt(dir, RoleWorker, "ts-abc123", false)
	if err == nil {
		t.Fatal("expected error for unresolved template variable, got nil")
	}
	if !strings.Contains(err.Error(), "unresolved template variable") {
		t.Errorf("error should mention unresolved variable, got: %v", err)
	}
}
