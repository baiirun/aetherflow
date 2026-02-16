package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/baiirun/aetherflow/internal/daemon"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// logStreamMsg carries newly read log lines from the file.
type logStreamMsg struct {
	lines    []string
	newCount int // updated raw line count for next read
	err      error
}

// logPathMsg carries the result of fetching the log file path from the daemon.
type logPathMsg struct {
	path string
	err  error
}

// LogStreamModel is the full-screen JSONL log viewer.
// It reads from the agent's log file directly, formats each line with
// daemon.FormatLogLine, and renders in a scrollable viewport.
// New lines are polled on each tick (every 2s from the parent).
type LogStreamModel struct {
	agentID string
	path    string // log file path (empty until fetched)
	pathErr error  // error fetching path

	vp         viewport.Model
	lines      []string // formatted lines
	lineCount  int      // total raw lines read so far (for incremental reads)
	autoScroll bool     // scroll to bottom on new content

	ready  bool
	width  int
	height int
}

const (
	logHeaderRows = 3 // header bar + blank line
	logFooterRows = 2 // blank line + help text
)

// NewLogStreamModel creates a new log stream viewer for the given agent.
func NewLogStreamModel(agentID string, width, height int) LogStreamModel {
	m := LogStreamModel{
		agentID:    agentID,
		width:      width,
		height:     height,
		autoScroll: true,
	}
	if width > 0 && height > 0 {
		m.initViewport()
	}
	return m
}

func (m *LogStreamModel) initViewport() {
	vpH := max(4, m.height-logHeaderRows-logFooterRows)
	m.vp = viewport.New(m.width-2, vpH) // -2 for left margin
	m.vp.SetContent(dimStyle.Render("Loading logs..."))
	m.ready = true
}

// Init returns a no-op — the parent kicks off the log path fetch.
func (m LogStreamModel) Init() tea.Cmd {
	return nil
}

// Update handles messages for the log stream screen.
func (m LogStreamModel) Update(msg tea.Msg) (LogStreamModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.initViewport()
		m.refreshContent()

	case tea.KeyMsg:
		switch msg.String() {
		case "G":
			// Jump to bottom and re-enable auto-scroll.
			m.autoScroll = true
			if m.ready {
				m.vp.GotoBottom()
			}
			return m, nil
		case "g":
			// Jump to top and disable auto-scroll.
			m.autoScroll = false
			if m.ready {
				m.vp.GotoTop()
			}
			return m, nil
		}
		// Disable auto-scroll if user scrolls manually.
		if msg.String() == "up" || msg.String() == "k" ||
			msg.String() == "pgup" || msg.String() == "ctrl+u" {
			m.autoScroll = false
		}

	case logPathMsg:
		if msg.err != nil {
			m.pathErr = msg.err
			if m.ready {
				m.vp.SetContent(redStyle.Render(fmt.Sprintf("Error: %v", msg.err)))
			}
			return m, nil
		}
		m.path = msg.path
		// Do an initial read immediately.
		return m, m.readNewLinesCmd()

	case logStreamMsg:
		if msg.err != nil {
			// Non-fatal — file may not exist yet. Show error but keep polling.
			return m, nil
		}
		m.lineCount = msg.newCount
		if len(msg.lines) > 0 {
			m.lines = append(m.lines, msg.lines...)
			m.refreshContent()
		}
	}

	// Forward to viewport for scroll handling.
	if m.ready {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	return m, nil
}

// refreshContent updates the viewport content from the accumulated lines.
func (m *LogStreamModel) refreshContent() {
	if !m.ready {
		return
	}
	if len(m.lines) == 0 {
		m.vp.SetContent(dimStyle.Render("No log output yet..."))
		return
	}
	m.vp.SetContent(strings.Join(m.lines, "\n"))
	if m.autoScroll {
		m.vp.GotoBottom()
	}
}

// readNewLinesCmd returns a Cmd that reads new lines from the log file.
// It reads from the current lineCount offset so we only get new data.
func (m LogStreamModel) readNewLinesCmd() tea.Cmd {
	path := m.path
	offset := m.lineCount
	return func() tea.Msg {
		result, err := readLogLines(path, offset)
		if err != nil {
			return logStreamMsg{err: err}
		}
		return logStreamMsg{lines: result.lines, newCount: result.newCount}
	}
}

// logReadResult holds formatted lines and the new total raw line count.
type logReadResult struct {
	lines    []string
	newCount int // total raw lines in file after this read
}

// readLogLines reads a JSONL file from the given line offset, formats
// each line with daemon.FormatLogLine, and returns the new formatted lines
// plus the updated total line count (so the next read can skip already-seen lines).
func readLogLines(path string, offset int) (*logReadResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var formatted []string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= offset {
			continue
		}
		line := scanner.Text()
		out := daemon.FormatLogLine([]byte(line))
		if out != "" {
			formatted = append(formatted, out)
		}
	}
	if err := scanner.Err(); err != nil {
		return &logReadResult{lines: formatted, newCount: lineNum}, err
	}
	return &logReadResult{lines: formatted, newCount: lineNum}, nil
}

// View renders the full-screen log stream.
func (m LogStreamModel) View() string {
	var b strings.Builder
	b.WriteString(m.viewHeader())
	b.WriteString("\n")

	if !m.ready {
		b.WriteString("  " + dimStyle.Render("Loading...") + "\n")
	} else {
		b.WriteString("  ")
		b.WriteString(m.vp.View())
		b.WriteString("\n")
	}

	b.WriteString(m.viewFooter())
	return b.String()
}

func (m LogStreamModel) viewHeader() string {
	return fmt.Sprintf("\n  %s  %s  %s\n",
		titleStyle.Render("aetherflow"),
		paneHeaderStyle.Render("Log Stream"),
		cyanStyle.Render(m.agentID),
	)
}

func (m LogStreamModel) viewFooter() string {
	scrollLabel := ""
	if m.ready {
		pct := m.vp.ScrollPercent() * 100
		scrollLabel = dimStyle.Render(fmt.Sprintf("  %.0f%%", pct))
	}
	autoLabel := ""
	if m.autoScroll {
		autoLabel = "  " + greenStyle.Render("[follow]")
	}
	return fmt.Sprintf("  %s%s%s\n",
		dimStyle.Render("j/k scroll  g top  G bottom+follow  q back"),
		scrollLabel,
		autoLabel,
	)
}
