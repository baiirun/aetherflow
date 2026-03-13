package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveDaemonURLUsesListenAddrFromConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".aetherflow.yaml")
	if err := os.WriteFile(configPath, []byte("listen_addr: :7099\nproject: ignored\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("project", "", "")
	if err := cmd.Flags().Set("config", configPath); err != nil {
		t.Fatal(err)
	}

	got := resolveDaemonURL(cmd)
	if got != "http://127.0.0.1:7099" {
		t.Fatalf("resolveDaemonURL = %q, want %q", got, "http://127.0.0.1:7099")
	}
}
