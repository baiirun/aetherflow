package protocol

import (
	"strings"
	"testing"
)

func TestDaemonURLFor(t *testing.T) {
	// Empty project returns the default URL.
	got := DaemonURLFor("")
	if got != DefaultDaemonURL {
		t.Errorf("DaemonURLFor(%q) = %q, want %q", "", got, DefaultDaemonURL)
	}

	// Non-empty project returns a URL with a port in the hashed range [7071, 7170].
	got = DaemonURLFor("myproject")
	if !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Errorf("DaemonURLFor(%q) = %q, expected http://127.0.0.1:PORT prefix", "myproject", got)
	}
	if got == DefaultDaemonURL {
		t.Errorf("DaemonURLFor(%q) should differ from default URL", "myproject")
	}

	// Same project always produces the same URL (deterministic).
	got2 := DaemonURLFor("myproject")
	if got != got2 {
		t.Errorf("DaemonURLFor is non-deterministic: %q vs %q", got, got2)
	}

	// Different projects produce different URLs (inputs are fixed; no collision expected).
	other := DaemonURLFor("other-project")
	if got == other {
		t.Errorf("unexpected hash collision between %q and %q", "myproject", "other-project")
	}
}

func TestNormalizeListenAddr(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: ":7070", want: "127.0.0.1:7070"},
		{in: "127.0.0.1:7071", want: "127.0.0.1:7071"},
		{in: "localhost:7072", want: "localhost:7072"},
		{in: "[::1]:7073", want: "[::1]:7073"},
	}

	for _, tt := range tests {
		got, err := NormalizeListenAddr(tt.in)
		if err != nil {
			t.Fatalf("NormalizeListenAddr(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("NormalizeListenAddr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDaemonURLFromListenAddr(t *testing.T) {
	got, err := DaemonURLFromListenAddr(":7070")
	if err != nil {
		t.Fatalf("DaemonURLFromListenAddr error = %v", err)
	}
	if got != "http://127.0.0.1:7070" {
		t.Fatalf("DaemonURLFromListenAddr = %q, want %q", got, "http://127.0.0.1:7070")
	}
}
