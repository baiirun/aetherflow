//go:build linux

package term

import (
	"os"
	"syscall"
	"unsafe"
)

// tcgets is the Linux ioctl for reading terminal attributes.
// On macOS/BSD this is TIOCGETA; on Linux it's TCGETS (0x5401).
const tcgets = 0x5401

// isTerminal reports whether f is connected to a terminal.
func isTerminal(f *os.File) bool {
	var termios syscall.Termios
	_, _, err := syscall.Syscall6(
		syscall.SYS_IOCTL,
		f.Fd(),
		tcgets,
		uintptr(unsafe.Pointer(&termios)),
		0, 0, 0,
	)
	return err == 0
}

// winsize matches the C struct winsize from sys/ioctl.h.
// Xpixel/Ypixel are required by the struct layout but unused.
type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// tiocgwinsz is the Linux ioctl for reading terminal window size.
const tiocgwinsz = 0x5413

// Width returns the terminal width in columns, or the fallback if detection fails.
func Width(fallback int) int {
	var ws winsize
	_, _, err := syscall.Syscall6(
		syscall.SYS_IOCTL,
		os.Stdout.Fd(),
		tiocgwinsz,
		uintptr(unsafe.Pointer(&ws)),
		0, 0, 0,
	)
	if err != 0 || ws.Col == 0 {
		return fallback
	}
	return int(ws.Col)
}
