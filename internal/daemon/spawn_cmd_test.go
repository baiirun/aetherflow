package daemon

import "testing"

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
