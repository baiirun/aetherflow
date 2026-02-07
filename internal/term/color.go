// Package term provides terminal color output and width detection.
//
// Colors are disabled when:
//   - NO_COLOR env var is set (any value, per https://no-color.org/)
//   - Disable(true) has been called (for --no-color flag)
//   - stdout is not a terminal (piped/redirected)
package term

import (
	"fmt"
	"os"
	"sync"
)

// ANSI color codes. These are the SGR (Select Graphic Rendition) sequences.
const (
	reset   = "\x1b[0m"
	bold    = "\x1b[1m"
	dim     = "\x1b[2m"
	red     = "\x1b[31m"
	green   = "\x1b[32m"
	yellow  = "\x1b[33m"
	blue    = "\x1b[34m"
	magenta = "\x1b[35m"
	cyan    = "\x1b[36m"
)

var (
	mu       sync.Mutex
	disabled bool

	initOnce sync.Once
	noColor  bool
)

// Disable forces colors off. This does not override environment detection —
// if NO_COLOR is set or stdout is not a terminal, colors remain off regardless.
// Call from --no-color flag handler.
func Disable(off bool) {
	mu.Lock()
	defer mu.Unlock()
	disabled = off
}

// enabled returns true if color output should be used.
func enabled() bool {
	// One-time environment detection. sync.Once uses an atomic fast path
	// after the first call, so subsequent calls have no lock overhead.
	initOnce.Do(func() {
		// NO_COLOR env var: any value means disable (https://no-color.org/).
		if _, ok := os.LookupEnv("NO_COLOR"); ok {
			noColor = true
			return
		}
		// Not a terminal: disable colors for piped/redirected output.
		if !isTerminal(os.Stdout) {
			noColor = true
		}
	})

	mu.Lock()
	defer mu.Unlock()
	return !disabled && !noColor
}

// wrap returns s wrapped in the given ANSI code, or s unchanged if colors are off.
func wrap(code, s string) string {
	if !enabled() {
		return s
	}
	return code + s + reset
}

// Green returns s in green (running agents, success).
func Green(s string) string { return wrap(green, s) }

// Red returns s in red (crashed agents, errors).
func Red(s string) string { return wrap(red, s) }

// Yellow returns s in yellow (queue items, warnings).
func Yellow(s string) string { return wrap(yellow, s) }

// Dim returns s in dim (idle slots, secondary info).
func Dim(s string) string { return wrap(dim, s) }

// Bold returns s in bold (headers, labels).
func Bold(s string) string { return wrap(bold, s) }

// Cyan returns s in cyan (agent IDs, identifiers).
func Cyan(s string) string { return wrap(cyan, s) }

// Blue returns s in blue (task IDs).
func Blue(s string) string { return wrap(blue, s) }

// Magenta returns s in magenta (roles).
func Magenta(s string) string { return wrap(magenta, s) }

// Greenf formats and returns the result in green.
func Greenf(format string, a ...any) string { return Green(fmt.Sprintf(format, a...)) }

// Redf formats and returns the result in red.
func Redf(format string, a ...any) string { return Red(fmt.Sprintf(format, a...)) }

// Yellowf formats and returns the result in yellow.
func Yellowf(format string, a ...any) string { return Yellow(fmt.Sprintf(format, a...)) }

// Dimf formats and returns the result in dim.
func Dimf(format string, a ...any) string { return Dim(fmt.Sprintf(format, a...)) }

// PadRight pads s with spaces to the given visible width, then wraps in color.
// Use this instead of %-Ns format verbs with colored strings, because fmt pads
// by byte length (which includes invisible ANSI codes) not visible width.
func PadRight(s string, width int, color func(string) string) string {
	runes := []rune(s)
	if len(runes) >= width {
		return color(s)
	}
	padded := s + spaces(width-len(runes))
	return color(padded)
}

// PadLeft pads s with leading spaces to the given visible width, then wraps in color.
func PadLeft(s string, width int, color func(string) string) string {
	runes := []rune(s)
	if len(runes) >= width {
		return color(s)
	}
	padded := spaces(width-len(runes)) + s
	return color(padded)
}

// spaces returns a string of n space characters.
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
