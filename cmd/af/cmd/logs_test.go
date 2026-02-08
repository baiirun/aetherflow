package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAllLines(t *testing.T) {
	path := writeTestFile(t, "line1\nline2\nline3\n")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	lines, err := readAllLines(f)
	if err != nil {
		t.Fatal(err)
	}

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" {
		t.Errorf("lines = %v, want [line1 line2 line3]", lines)
	}
}

func TestReadAllLinesEmpty(t *testing.T) {
	path := writeTestFile(t, "")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	lines, err := readAllLines(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("got %d lines, want 0", len(lines))
	}
}

func TestTailFileLastN(t *testing.T) {
	// Create a test file with 10 lines.
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	path := writeTestFile(t, content)

	// Capture stdout — tailFile writes to stdout.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := tailFile(path, 3, false, false)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatal(err)
	}

	output, _ := io.ReadAll(r)

	// Should show last 3 lines.
	outputLines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(outputLines) != 3 {
		t.Fatalf("got %d output lines, want 3: %v", len(outputLines), outputLines)
	}
}

func TestTailFileAllLines(t *testing.T) {
	// Request more lines than exist — should show all lines.
	content := "a\nb\nc\n"
	path := writeTestFile(t, content)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := tailFile(path, 100, false, false)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatal(err)
	}

	output, _ := io.ReadAll(r)

	outputLines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(outputLines) != 3 {
		t.Fatalf("got %d output lines, want 3: %v", len(outputLines), outputLines)
	}
}

func TestTailFileNotFound(t *testing.T) {
	err := tailFile("/nonexistent/file.jsonl", 10, false, false)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// writeTestFile creates a temp file with the given content and returns its path.
func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
