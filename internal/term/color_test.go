package term

import (
	"os"
	"sync"
	"testing"
)

// resetState clears cached color detection so each test starts fresh.
func resetState() {
	mu.Lock()
	disabled = false
	mu.Unlock()

	// Replace the sync.Once so the next enabled() call re-detects.
	initOnce = sync.Once{}
	noColor = false
}

func TestDisableForcesColorsOff(t *testing.T) {
	resetState()
	defer resetState()

	Disable(true)

	got := Green("hello")
	if got != "hello" {
		t.Errorf("Green() with Disable(true) = %q, want %q", got, "hello")
	}
}

func TestDisableCanBeReenabled(t *testing.T) {
	resetState()
	defer resetState()

	Disable(true)
	if got := Green("x"); got != "x" {
		t.Errorf("Green() with Disable(true) = %q, want %q", got, "x")
	}

	Disable(false)
	// After re-enabling, result depends on environment (terminal/NO_COLOR).
	// Just verify it doesn't panic and returns something.
	_ = Green("hello")
}

func TestNO_COLOREnvDisablesColors(t *testing.T) {
	resetState()
	defer resetState()

	t.Setenv("NO_COLOR", "1")

	got := Green("hello")
	if got != "hello" {
		t.Errorf("Green() with NO_COLOR=1 = %q, want %q", got, "hello")
	}
}

func TestNO_COLOREmptyValueDisablesColors(t *testing.T) {
	resetState()
	defer resetState()

	t.Setenv("NO_COLOR", "")

	got := Green("hello")
	if got != "hello" {
		t.Errorf("Green() with NO_COLOR= = %q, want %q", got, "hello")
	}
}

func TestColorFunctionsReturnPlainWhenDisabled(t *testing.T) {
	resetState()
	defer resetState()

	Disable(true)

	tests := []struct {
		name string
		fn   func(string) string
	}{
		{"Green", Green},
		{"Red", Red},
		{"Yellow", Yellow},
		{"Dim", Dim},
		{"Bold", Bold},
		{"Cyan", Cyan},
		{"Blue", Blue},
		{"Magenta", Magenta},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn("test")
			if got != "test" {
				t.Errorf("%s(\"test\") with colors disabled = %q, want %q", tt.name, got, "test")
			}
		})
	}
}

func TestFormatFunctionsReturnPlainWhenDisabled(t *testing.T) {
	resetState()
	defer resetState()

	Disable(true)

	tests := []struct {
		name string
		fn   func(string, ...any) string
	}{
		{"Greenf", Greenf},
		{"Redf", Redf},
		{"Yellowf", Yellowf},
		{"Dimf", Dimf},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn("count=%d", 42)
			if got != "count=42" {
				t.Errorf("%s(\"count=%%d\", 42) = %q, want %q", tt.name, got, "count=42")
			}
		})
	}
}

func TestColorOutputWhenEnabled(t *testing.T) {
	resetState()
	defer resetState()

	// Force the init path to detect "enabled" by ensuring no NO_COLOR
	// and marking detection as complete with noColor=false.
	initOnce.Do(func() {
		noColor = false
	})

	got := Green("hi")
	want := "\x1b[32mhi\x1b[0m"
	if got != want {
		t.Errorf("Green(\"hi\") = %q, want %q", got, want)
	}

	got = Bold("x")
	want = "\x1b[1mx\x1b[0m"
	if got != want {
		t.Errorf("Bold(\"x\") = %q, want %q", got, want)
	}
}

func TestPipedOutputDisablesColors(t *testing.T) {
	resetState()
	defer resetState()

	// When stdout is not a terminal (e.g., piped in CI), colors should be off.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	if isTerminal(w) {
		t.Error("isTerminal(pipe) = true, want false")
	}
}

func TestWidthReturnsFallback(t *testing.T) {
	// In CI/piped environments, Width should return the fallback.
	// On a real terminal it returns the actual width.
	// Either way, the result should be > 0.
	w := Width(80)
	if w <= 0 {
		t.Errorf("Width(80) = %d, want > 0", w)
	}
}

func TestPadRight(t *testing.T) {
	resetState()
	defer resetState()
	Disable(true)

	tests := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"shorter", "abc", 6, "abc   "},
		{"exact", "abcdef", 6, "abcdef"},
		{"longer", "abcdefgh", 6, "abcdefgh"},
		{"empty", "", 4, "    "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PadRight(tt.s, tt.width, Green) // Green is no-op when disabled
			if got != tt.want {
				t.Errorf("PadRight(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
		})
	}
}

func TestPadRightWithColor(t *testing.T) {
	resetState()
	defer resetState()

	// Force colors on.
	initOnce.Do(func() { noColor = false })

	got := PadRight("ab", 5, Green)
	// Should be: green("ab   ") = \x1b[32mab   \x1b[0m
	want := "\x1b[32mab   \x1b[0m"
	if got != want {
		t.Errorf("PadRight with color = %q, want %q", got, want)
	}
}

func TestPadLeft(t *testing.T) {
	resetState()
	defer resetState()
	Disable(true)

	got := PadLeft("42", 6, Green)
	want := "    42"
	if got != want {
		t.Errorf("PadLeft(%q, 6) = %q, want %q", "42", got, want)
	}
}
