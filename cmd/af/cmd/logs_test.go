package cmd

import (
	"testing"
)

func TestLogsFlagsRegistered(t *testing.T) {
	// Verify streaming flags are registered on the logs command.
	f := logsCmd.Flags()

	if f.Lookup("follow") == nil {
		t.Error("--follow flag not registered")
	}
	if f.ShorthandLookup("f") == nil {
		t.Error("-f shorthand not registered")
	}
	if f.Lookup("watch") == nil {
		t.Error("--watch flag not registered")
	}
	if f.ShorthandLookup("w") == nil {
		t.Error("-w shorthand not registered")
	}
	if f.Lookup("lines") == nil {
		t.Error("--lines flag not registered")
	}
	if f.ShorthandLookup("n") == nil {
		t.Error("-n shorthand not registered")
	}
	if f.Lookup("raw") == nil {
		t.Error("--raw flag not registered")
	}
}

func TestLogsFlagAliases(t *testing.T) {
	// Both --follow and --watch should be accepted by the logs command.
	// They don't need to be synchronized at the flag level - the Run function
	// ORs them together to determine streaming behavior.
	f := logsCmd.Flags()

	// Test --follow flag
	if err := f.Set("follow", "true"); err != nil {
		t.Fatalf("failed to set --follow: %v", err)
	}
	follow, _ := f.GetBool("follow")
	if !follow {
		t.Error("--follow should be true after setting")
	}

	// Reset and test --watch flag
	_ = f.Set("follow", "false")
	if err := f.Set("watch", "true"); err != nil {
		t.Fatalf("failed to set --watch: %v", err)
	}
	watch, _ := f.GetBool("watch")
	if !watch {
		t.Error("--watch should be true after setting")
	}

	// Both flags should work with their shorthands
	_ = f.Set("watch", "false")
	if err := logsCmd.ParseFlags([]string{"-f"}); err != nil {
		t.Fatalf("failed to parse -f: %v", err)
	}
	follow, _ = f.GetBool("follow")
	if !follow {
		t.Error("-f should set --follow to true")
	}

	_ = f.Set("follow", "false")
	if err := logsCmd.ParseFlags([]string{"-w"}); err != nil {
		t.Fatalf("failed to parse -w: %v", err)
	}
	watch, _ = f.GetBool("watch")
	if !watch {
		t.Error("-w should set --watch to true")
	}
}

func TestDefaultTailLines(t *testing.T) {
	if defaultTailLines != 20 {
		t.Errorf("defaultTailLines = %d, want 20", defaultTailLines)
	}
}
