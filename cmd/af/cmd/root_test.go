package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/baiirun/aetherflow/internal/protocol"
	"github.com/spf13/cobra"
)

func TestResolveDaemonURLUsesListenAddrFromConfig(t *testing.T) {
	configPath := writeResolveConfig(t, "listen_addr: :7099\nproject: ignored\n")

	cmd := newResolveTestCommand(t, configPath)

	got := resolveDaemonURL(cmd)
	if got != "http://127.0.0.1:7099" {
		t.Fatalf("resolveDaemonURL = %q, want %q", got, "http://127.0.0.1:7099")
	}
}

func TestResolveDaemonURLIgnoresConfigProjectInManualMode(t *testing.T) {
	configPath := writeResolveConfig(t, "project: from-file\nspawn_policy: manual\n")

	cmd := newResolveTestCommand(t, configPath)

	got := resolveDaemonURL(cmd)
	if got != protocol.DefaultDaemonURL {
		t.Fatalf("resolveDaemonURL = %q, want %q", got, protocol.DefaultDaemonURL)
	}
}

func TestResolveDaemonURLUsesConfigProjectInAutoMode(t *testing.T) {
	configPath := writeResolveConfig(t, "project: from-file\nspawn_policy: auto\n")

	cmd := newResolveTestCommand(t, configPath)

	got := resolveDaemonURL(cmd)
	want := protocol.DaemonURLFor("from-file")
	if got != want {
		t.Fatalf("resolveDaemonURL = %q, want %q", got, want)
	}
}

func TestResolveDaemonURLUsesExplicitProjectInManualMode(t *testing.T) {
	cmd := newResolveTestCommand(t, "")
	if err := cmd.Flags().Set("project", "manual-target"); err != nil {
		t.Fatal(err)
	}

	got := resolveDaemonURL(cmd)
	want := protocol.DaemonURLFor("manual-target")
	if got != want {
		t.Fatalf("resolveDaemonURL = %q, want %q", got, want)
	}
}

func TestResolveDaemonURLUsesExplicitProjectInAutoMode(t *testing.T) {
	cmd := newResolveTestCommand(t, "")
	if err := cmd.Flags().Set("project", "auto-target"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("spawn-policy", "auto"); err != nil {
		t.Fatal(err)
	}

	got := resolveDaemonURL(cmd)
	want := protocol.DaemonURLFor("auto-target")
	if got != want {
		t.Fatalf("resolveDaemonURL = %q, want %q", got, want)
	}
}

func newResolveTestCommand(t *testing.T, configPath string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("spawn-policy", "", "")
	if configPath != "" {
		if err := cmd.Flags().Set("config", configPath); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

func writeResolveConfig(t *testing.T, contents string) string {
	t.Helper()

	dir := t.TempDir()
	configPath := filepath.Join(dir, ".aetherflow.yaml")
	if err := os.WriteFile(configPath, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	return configPath
}
