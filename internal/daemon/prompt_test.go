package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPrompt(t *testing.T) {
	dir := t.TempDir()

	content := "# Worker\n\nTask: {{task_id}}\n\nRun: prog show {{task_id}}\n"
	if err := os.WriteFile(filepath.Join(dir, "worker.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := RenderPrompt(dir, RoleWorker, "ts-abc123")
	if err != nil {
		t.Fatalf("RenderPrompt returned error: %v", err)
	}

	want := "# Worker\n\nTask: ts-abc123\n\nRun: prog show ts-abc123\n"
	if got != want {
		t.Errorf("RenderPrompt mismatch\ngot:  %q\nwant: %q", got, want)
	}

	// Verify no unreplaced template variables remain.
	if strings.Contains(got, "{{task_id}}") {
		t.Error("rendered prompt still contains {{task_id}}")
	}
}

func TestRenderPromptMissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := RenderPrompt(dir, RoleWorker, "ts-abc123")
	if err == nil {
		t.Fatal("expected error for missing prompt file, got nil")
	}

	if !strings.Contains(err.Error(), "worker.md") {
		t.Errorf("error should mention the filename, got: %v", err)
	}
}

func TestRenderPromptUnknownRole(t *testing.T) {
	dir := t.TempDir()

	_, err := RenderPrompt(dir, Role("hacker"), "ts-abc123")
	if err == nil {
		t.Fatal("expected error for unknown role, got nil")
	}

	if !strings.Contains(err.Error(), "unknown role") {
		t.Errorf("error should mention unknown role, got: %v", err)
	}
}

func TestRenderPromptUnresolvedVariable(t *testing.T) {
	dir := t.TempDir()

	// Template with a typo: "{{ task_id }}" (spaces) won't be replaced.
	content := "# Worker\n\nTask: {{ task_id }}\n"
	if err := os.WriteFile(filepath.Join(dir, "worker.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := RenderPrompt(dir, RoleWorker, "ts-abc123")
	if err == nil {
		t.Fatal("expected error for unresolved template variable, got nil")
	}

	if !strings.Contains(err.Error(), "unresolved template variable") {
		t.Errorf("error should mention unresolved variable, got: %v", err)
	}
}
