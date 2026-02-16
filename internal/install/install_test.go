package install

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// countEmbeddedFiles returns the number of files in the embedded asset FS.
// Tests use this instead of a hardcoded count so they don't break when
// skills or agents are added or removed.
func countEmbeddedFiles(t *testing.T) int {
	t.Helper()
	var count int
	err := fs.WalkDir(assetsFS, ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk embedded assets: %v", err)
	}
	if count == 0 {
		t.Fatal("embedded assets contain zero files — build is broken")
	}
	return count
}

// TestPlanReturnsAllAssets verifies that Plan walks the embedded FS and
// returns an action for every bundled skill and agent file.
func TestPlanReturnsAllAssets(t *testing.T) {
	target := t.TempDir()
	actions, err := Plan(target)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	want := countEmbeddedFiles(t)
	if len(actions) != want {
		t.Errorf("expected %d actions, got %d", want, len(actions))
		for _, a := range actions {
			t.Logf("  %s", a.RelPath)
		}
	}

	// All should be ActionWrite on a fresh directory.
	for _, a := range actions {
		if a.Action != ActionWrite {
			t.Errorf("expected ActionWrite for %s on fresh target, got %s", a.RelPath, a.Action)
		}
	}
}

// TestExecuteFreshInstall verifies that Execute writes all files to an empty
// target directory.
func TestExecuteFreshInstall(t *testing.T) {
	target := t.TempDir()
	want := countEmbeddedFiles(t)

	actions, err := Plan(target)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	result := Execute(actions)

	if result.Written != want {
		t.Errorf("expected %d written, got %d", want, result.Written)
	}
	if result.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", result.Skipped)
	}
	if result.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", result.Errors)
	}

	// Verify files actually exist on disk.
	wantFiles := []string{
		"skills/review-auto/SKILL.md",
		"skills/compound-auto/SKILL.md",
		"agents/code-reviewer.md",
		"agents/code-simplicity-reviewer.md",
		"agents/security-sentinel.md",
		"agents/architecture-strategist.md",
		"agents/grug-brain-reviewer.md",
		"agents/tigerstyle-reviewer.md",
		"agents/performance-oracle.md",
		"agents/agent-native-reviewer.md",
	}
	for _, f := range wantFiles {
		path := filepath.Join(target, f)
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Errorf("expected file %s to exist: %v", f, statErr)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("expected file %s to have content, got 0 bytes", f)
		}
	}
}

// TestExecuteIdempotent verifies that running Plan+Execute twice produces the
// same result: the second run skips all files.
func TestExecuteIdempotent(t *testing.T) {
	target := t.TempDir()
	want := countEmbeddedFiles(t)

	// First install.
	actions, err := Plan(target)
	if err != nil {
		t.Fatalf("first Plan returned error: %v", err)
	}
	Execute(actions)

	// Second install — everything should be skipped.
	actions2, err := Plan(target)
	if err != nil {
		t.Fatalf("second Plan returned error: %v", err)
	}
	result := Execute(actions2)

	if result.Written != 0 {
		t.Errorf("expected 0 written on second run, got %d", result.Written)
	}
	if result.Skipped != want {
		t.Errorf("expected %d skipped on second run, got %d", want, result.Skipped)
	}
	if result.Errors != 0 {
		t.Errorf("expected 0 errors on second run, got %d", result.Errors)
	}
}

// TestExecuteUpdate verifies that modified files are overwritten.
func TestExecuteUpdate(t *testing.T) {
	target := t.TempDir()

	// First install.
	actions, err := Plan(target)
	if err != nil {
		t.Fatalf("first Plan returned error: %v", err)
	}
	Execute(actions)

	// Modify one file.
	modifiedPath := filepath.Join(target, "agents", "code-reviewer.md")
	if err := os.WriteFile(modifiedPath, []byte("modified content"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	// Second install — should write the modified file, skip the rest.
	actions2, err := Plan(target)
	if err != nil {
		t.Fatalf("second Plan returned error: %v", err)
	}
	result := Execute(actions2)

	if result.Written != 1 {
		t.Errorf("expected 1 written (the modified file), got %d", result.Written)
	}
	want := countEmbeddedFiles(t)
	if result.Skipped != want-1 {
		t.Errorf("expected %d skipped, got %d", want-1, result.Skipped)
	}

	// Verify the file was restored to the embedded content.
	content, readErr := os.ReadFile(modifiedPath)
	if readErr != nil {
		t.Fatalf("failed to read restored file: %v", readErr)
	}
	if string(content) == "modified content" {
		t.Error("file should have been overwritten with embedded content")
	}
}

// TestPlanOnlyDoesNotWrite verifies that Plan alone (the dry-run equivalent)
// does not create any files on disk.
func TestPlanOnlyDoesNotWrite(t *testing.T) {
	target := t.TempDir()
	want := countEmbeddedFiles(t)

	actions, err := Plan(target)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	// All should be writes on a fresh target.
	var writeCount int
	for _, a := range actions {
		if a.Action == ActionWrite {
			writeCount++
		}
	}
	if writeCount != want {
		t.Errorf("expected %d writes in plan, got %d", want, writeCount)
	}

	// But no files should exist — Plan does not write.
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("failed to read target dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Plan should not create files, but found %d entries", len(entries))
	}
}

// TestExecuteReadOnlyTarget verifies that permission errors are reported
// per-file without stopping the entire install.
func TestExecuteReadOnlyTarget(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root ignores directory permissions")
	}

	target := t.TempDir()

	// Create the agents directory as read-only.
	agentsDir := filepath.Join(target, "agents")
	if err := os.MkdirAll(agentsDir, 0555); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(agentsDir, 0755) })

	actions, err := Plan(target)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	result := Execute(actions)

	// Skills should succeed, agents should fail.
	if result.Errors == 0 {
		t.Error("expected errors from read-only agents directory")
	}
	if result.Written == 0 {
		t.Error("expected some files to succeed (skills)")
	}
}

// TestPlanSkipsUpToDate verifies that Plan reports ActionSkip for files
// that match the embedded content.
func TestPlanSkipsUpToDate(t *testing.T) {
	target := t.TempDir()

	// Install everything first.
	actions, err := Plan(target)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	Execute(actions)

	// Plan should show all as skip.
	actions2, err := Plan(target)
	if err != nil {
		t.Fatalf("second Plan returned error: %v", err)
	}

	for _, a := range actions2 {
		if a.Action != ActionSkip {
			t.Errorf("expected ActionSkip for %s after install, got %s", a.RelPath, a.Action)
		}
	}
}
