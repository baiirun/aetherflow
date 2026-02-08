package protocol

import "testing"

func TestSocketPathFor(t *testing.T) {
	tests := []struct {
		project string
		want    string
	}{
		// Normal cases
		{"", DefaultSocketPath},
		{"myproject", "/tmp/aetherd-myproject.sock"},
		{"eldspire-hexmap", "/tmp/aetherd-eldspire-hexmap.sock"},
		{"my.project", "/tmp/aetherd-my.project.sock"},

		// Path traversal — filepath.Base strips directory components.
		{"../etc", "/tmp/aetherd-etc.sock"},
		{"../../run/systemd", "/tmp/aetherd-systemd.sock"},
		{"/absolute/path", "/tmp/aetherd-path.sock"},
		{"a/b/c", "/tmp/aetherd-c.sock"},

		// Degenerate inputs that filepath.Base collapses.
		{".", DefaultSocketPath},
		{"/", DefaultSocketPath},
	}
	for _, tt := range tests {
		got := SocketPathFor(tt.project)
		if got != tt.want {
			t.Errorf("SocketPathFor(%q) = %q, want %q", tt.project, got, tt.want)
		}
	}
}
