package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintDaemonNotRunning(t *testing.T) {
	var buf bytes.Buffer
	printDaemonNotRunning(&buf)
	out := buf.String()

	if !strings.Contains(out, "not running") {
		t.Fatalf("output = %q, want not running", out)
	}
	if !strings.Contains(out, "af daemon start --project <name>") {
		t.Fatalf("output = %q, want start hint", out)
	}
}
