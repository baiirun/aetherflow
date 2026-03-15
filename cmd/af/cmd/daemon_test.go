package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

func TestBuildConfigUsesExplicitListenAddrOverride(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("listen-addr", "", "")
	cmd.Flags().String("config", "", "")

	if err := cmd.Flags().Set("listen-addr", "127.0.0.1:7099"); err != nil {
		t.Fatalf("set listen-addr: %v", err)
	}
	if err := cmd.Flags().Set("config", filepath.Join(t.TempDir(), "missing.yaml")); err != nil {
		t.Fatalf("set config: %v", err)
	}

	cfg := buildConfig(cmd)
	if cfg.ListenAddr != "127.0.0.1:7099" {
		t.Fatalf("listen_addr = %q, want 127.0.0.1:7099", cfg.ListenAddr)
	}
	if cfg.SpawnPolicy != "manual" {
		t.Fatalf("spawn_policy = %q, want manual", cfg.SpawnPolicy)
	}
}
