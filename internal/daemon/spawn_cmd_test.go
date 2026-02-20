package daemon

import (
	"strings"
	"testing"
)

func TestEnsureAttachSpawnCmd(t *testing.T) {
	t.Parallel()

	got := EnsureAttachSpawnCmd("opencode run --format json", "http://127.0.0.1:4096")
	want := "opencode run --format json --attach http://127.0.0.1:4096"
	if got != want {
		t.Fatalf("EnsureAttachSpawnCmd() = %q, want %q", got, want)
	}
}

func TestEnsureAttachSpawnCmdNoopWhenPresent(t *testing.T) {
	t.Parallel()

	cmd := "opencode run --attach http://127.0.0.1:4096 --format json"
	got := EnsureAttachSpawnCmd(cmd, "http://127.0.0.1:5000")
	if got != cmd {
		t.Fatalf("EnsureAttachSpawnCmd() = %q, want %q", got, cmd)
	}
}

func TestEnsureAttachSpawnCmdDoesNotFalseMatch(t *testing.T) {
	t.Parallel()

	cmd := "opencode run --attachment-token xyz --format json"
	got := EnsureAttachSpawnCmd(cmd, "http://127.0.0.1:4096")
	want := "opencode run --attachment-token xyz --format json --attach http://127.0.0.1:4096"
	if got != want {
		t.Fatalf("EnsureAttachSpawnCmd() = %q, want %q", got, want)
	}
}

func TestWithSessionFlag(t *testing.T) {
	t.Parallel()

	cmd := "opencode run --attach http://127.0.0.1:4096 --format json"
	got := WithSessionFlag(cmd, "ses_abc123")
	want := "opencode run --attach http://127.0.0.1:4096 --format json --session ses_abc123"
	if got != want {
		t.Fatalf("WithSessionFlag() = %q, want %q", got, want)
	}
}

func TestWithSessionFlagEmptyID(t *testing.T) {
	t.Parallel()

	cmd := "opencode run --attach http://127.0.0.1:4096 --format json"
	got := WithSessionFlag(cmd, "")
	if got != cmd {
		t.Fatalf("WithSessionFlag() with empty ID = %q, want %q (unchanged)", got, cmd)
	}
}

func TestWithSessionFlagRejectsMalformed(t *testing.T) {
	t.Parallel()

	cmd := "opencode run --attach http://127.0.0.1:4096"
	tests := []struct {
		name      string
		sessionID string
	}{
		{"whitespace", "ses abc"},
		{"shell metachar", "ses;echo pwned"},
		{"pipe", "ses|cat"},
		{"path traversal", "../../etc/passwd"},
		{"newline", "ses\nabc"},
		{"backtick", "ses`id`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WithSessionFlag(cmd, tt.sessionID)
			if got != cmd {
				t.Errorf("WithSessionFlag(%q) should return unchanged cmd, got %q", tt.sessionID, got)
			}
		})
	}
}

func TestIsValidSessionID(t *testing.T) {
	t.Parallel()

	valid := []string{"ses_abc123", "ses-def-456", "ABC_xyz", "a"}
	for _, id := range valid {
		if !isValidSessionID(id) {
			t.Errorf("isValidSessionID(%q) = false, want true", id)
		}
	}

	invalid := []string{"", "has space", "semi;colon", strings.Repeat("a", 129)}
	for _, id := range invalid {
		if isValidSessionID(id) {
			t.Errorf("isValidSessionID(%q) = true, want false", id)
		}
	}
}
