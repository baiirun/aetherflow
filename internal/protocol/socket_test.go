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

	// Different projects produce different URLs (in almost all cases).
	other := DaemonURLFor("other-project")
	if got == other {
		t.Logf("hash collision between %q and %q (acceptable but unlikely)", "myproject", "other-project")
	}
}
