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
