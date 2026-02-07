package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/geobrowser/aetherflow/internal/client"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <agent-name>",
	Short: "Tail an agent's JSONL log",
	Long: `Stream the raw JSONL log for a running agent.

The daemon returns the log file path and the CLI tails it directly.
By default shows the last 20 lines. Use -n to change the initial count
and -f to follow new output as it's written.

Requires a running daemon and an active agent.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		socketPath, _ := cmd.Flags().GetString("socket")
		follow, _ := cmd.Flags().GetBool("follow")
		lines, _ := cmd.Flags().GetInt("lines")

		c := client.New(socketPath)
		path, err := c.LogsPath(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		if err := tailFile(path, lines, follow); err != nil {
			fmt.Fprintf(os.Stderr, "error tailing log: %v\n", err)
			os.Exit(1)
		}
	},
}

const defaultTailLines = 20

// tailFile prints the last n lines of a file, optionally following new output.
func tailFile(path string, n int, follow bool) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer f.Close()

	// Read all lines to get the tail. For agent logs this is fine —
	// they're bounded by session length and typically < 10k lines.
	lines, err := readAllLines(f)
	if err != nil {
		return err
	}

	// Print last n lines.
	start := 0
	if n > 0 && n < len(lines) {
		start = len(lines) - n
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}

	if !follow {
		return nil
	}

	// Follow mode: poll for new data until interrupted.
	return followFile(f)
}

// readAllLines reads all lines from the current position in the reader.
func readAllLines(r io.Reader) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(r)
	// Match the buffer size from ParseToolCalls — tool results can be large.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading log file: %w", err)
	}
	return lines, nil
}

const followPollInterval = 200 * time.Millisecond

// followFile polls a file for new lines and prints them until interrupted.
// The file must already be positioned at the point where new lines will appear
// (i.e., after reading the initial tail).
//
// Raw output — no ANSI stripping. This is intentional: logs is a raw
// viewer like tail(1). Use `af status <agent>` for sanitized output.
func followFile(f *os.File) error {
	fmt.Fprintf(os.Stderr, "following %s (poll every %s, ctrl-c to stop)\n", f.Name(), followPollInterval)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	reader := bufio.NewReader(f)
	ticker := time.NewTicker(followPollInterval)
	defer ticker.Stop()

	for {
		// Drain any available lines before waiting.
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				// ReadString includes the delimiter on complete lines and omits
				// it on partial reads at EOF. fmt.Print handles both correctly.
				fmt.Print(line)
			}
			if err != nil {
				if err != io.EOF {
					return fmt.Errorf("reading log file during follow: %w", err)
				}
				break // EOF — no more data right now; poll again.
			}
		}

		select {
		case <-sigCh:
			fmt.Println() // clean line after ^C
			return nil
		case <-ticker.C:
			// Continue to next read attempt.
		}
	}
}

func init() {
	rootCmd.AddCommand(logsCmd)

	logsCmd.Flags().String("socket", "", "Unix socket path (default: /tmp/aetherd.sock)")
	logsCmd.Flags().BoolP("follow", "f", false, "Follow new output as it's written")
	logsCmd.Flags().IntP("lines", "n", defaultTailLines, "Number of initial lines to show")
}
