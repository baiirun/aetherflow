package cmd

import (
	"testing"
	"time"
)

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name      string
		spawnTime time.Time
		want      string
	}{
		{
			name:      "seconds",
			spawnTime: time.Now().Add(-30 * time.Second),
			want:      "30s",
		},
		{
			name:      "minutes",
			spawnTime: time.Now().Add(-12 * time.Minute),
			want:      "12m",
		},
		{
			name:      "hours and minutes",
			spawnTime: time.Now().Add(-1*time.Hour - 30*time.Minute),
			want:      "1h30m",
		},
		{
			name:      "exact hours",
			spawnTime: time.Now().Add(-2 * time.Hour),
			want:      "2h",
		},
		{
			name:      "days",
			spawnTime: time.Now().Add(-26 * time.Hour),
			want:      "1d2h",
		},
		{
			name:      "zero time",
			spawnTime: time.Time{},
			want:      "?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatUptime(tt.spawnTime)
			if got != tt.want {
				t.Errorf("formatUptime() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly ten", 11, "exactly ten"},
		{"this string is way too long", 10, "this stri\u2026"},
		{"", 10, ""},
		{"hello\U0001F680world!", 8, "hello\U0001F680w\u2026"}, // multi-byte: rocket emoji is one rune
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

func TestQuote(t *testing.T) {
	if got := quote("hello"); got != `"hello"` {
		t.Errorf("quote(%q) = %q, want %q", "hello", got, `"hello"`)
	}
	if got := quote(""); got != "" {
		t.Errorf("quote(%q) = %q, want empty", "", got)
	}
}

func TestFormatRelativeTime(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"seconds ago", time.Now().Add(-15 * time.Second), "15s ago"},
		{"minutes ago", time.Now().Add(-5 * time.Minute), "5m ago"},
		{"hours ago", time.Now().Add(-2 * time.Hour), "2h ago"},
		{"hours and minutes", time.Now().Add(-1*time.Hour - 30*time.Minute), "1h30m"},
		{"zero time", time.Time{}, "?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRelativeTime(tt.t)
			if got != tt.want {
				t.Errorf("formatRelativeTime() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatusFlagsRegistered(t *testing.T) {
	// Verify watch-related flags are registered on the status command.
	f := statusCmd.Flags()

	if f.Lookup("watch") == nil {
		t.Error("--watch flag not registered")
	}
	if f.ShorthandLookup("w") == nil {
		t.Error("-w shorthand not registered")
	}
	if f.Lookup("follow") == nil {
		t.Error("--follow flag not registered")
	}
	if f.ShorthandLookup("f") == nil {
		t.Error("-f shorthand not registered")
	}
	if f.Lookup("interval") == nil {
		t.Error("--interval flag not registered")
	}

	// Default interval should be 2s.
	interval, err := f.GetDuration("interval")
	if err != nil {
		t.Fatalf("GetDuration(interval): %v", err)
	}
	if interval != 2*time.Second {
		t.Errorf("default interval = %v, want 2s", interval)
	}
}

func TestStatusFlagAliases(t *testing.T) {
	// Both --watch and --follow should be accepted by the status command.
	// They don't need to be synchronized at the flag level - the Run function
	// ORs them together to determine streaming behavior.
	f := statusCmd.Flags()

	// Test --watch flag
	if err := f.Set("watch", "true"); err != nil {
		t.Fatalf("failed to set --watch: %v", err)
	}
	watch, _ := f.GetBool("watch")
	if !watch {
		t.Error("--watch should be true after setting")
	}

	// Reset and test --follow flag
	f.Set("watch", "false")
	if err := f.Set("follow", "true"); err != nil {
		t.Fatalf("failed to set --follow: %v", err)
	}
	follow, _ := f.GetBool("follow")
	if !follow {
		t.Error("--follow should be true after setting")
	}

	// Both flags should work with their shorthands
	f.Set("follow", "false")
	if err := statusCmd.ParseFlags([]string{"-w"}); err != nil {
		t.Fatalf("failed to parse -w: %v", err)
	}
	watch, _ = f.GetBool("watch")
	if !watch {
		t.Error("-w should set --watch to true")
	}

	f.Set("watch", "false")
	if err := statusCmd.ParseFlags([]string{"-f"}); err != nil {
		t.Fatalf("failed to parse -f: %v", err)
	}
	follow, _ = f.GetBool("follow")
	if !follow {
		t.Error("-f should set --follow to true")
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no escapes", "hello world", "hello world"},
		{"empty", "", ""},
		{"color codes", "\x1b[31mred text\x1b[0m", "red text"},
		{"clear screen", "\x1b[2Jhello", "hello"},
		{"window title", "\x1b]0;evil title\x07normal", "normal"},
		{"mixed", "before\x1b[1mbolded\x1b[0mafter", "beforeboldedafter"},
		{"carriage return", "overwrite\rvisible", "overwritevisible"},
		{"backspace", "typo\x08fixed", "typofixed"},
		{"delete char", "test\x7fmore", "testmore"},
		{"DCS sequence", "\x1bPq#0;2;0;0;0#1;2;100;100;0\x1b\\done", "done"},
		{"null byte", "before\x00after", "beforeafter"},
		{"preserves tabs", "col1\tcol2", "col1\tcol2"},
		{"preserves newlines", "line1\nline2", "line1\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.input)
			if got != tt.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
