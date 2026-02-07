//go:build windows

package term

import "os"

// isTerminal returns false on Windows — colors are disabled by default.
// Windows terminal color support could be added via the Console API if needed.
func isTerminal(f *os.File) bool { return false }

// Width returns the fallback on Windows.
func Width(fallback int) int { return fallback }
