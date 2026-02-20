package tui

import (
	"fmt"
	"strings"

	"github.com/baiirun/aetherflow/internal/client"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// logEventsMsg carries newly fetched event lines from the daemon RPC.
type logEventsMsg struct {
	lines  []string
	lastTS int64 // for incremental polling
	err    error
}

// LogStreamModel is the full-screen event log viewer.
// It reads events from the daemon's in-memory buffer via the events.list RPC,
// formats them, and renders in a scrollable viewport.
// New events are polled on each tick (every 2s from the parent).
type LogStreamModel struct {
	agentID string
	client  *client.Client

	vp         viewport.Model
	lines      []string // formatted lines
	lastTS     int64    // last event timestamp for incremental reads
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
func NewLogStreamModel(agentID string, c *client.Client, width, height int) LogStreamModel {
	m := LogStreamModel{
		agentID:    agentID,
		client:     c,
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
	m.vp.SetContent(dimStyle.Render("Loading events..."))
	m.ready = true
}

// Init returns the initial fetch command.
func (m LogStreamModel) Init() tea.Cmd {
	return m.fetchEventsCmd()
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

	case logEventsMsg:
		if msg.err != nil {
			// Non-fatal — daemon may be temporarily unavailable.
			return m, nil
		}
		if msg.lastTS > m.lastTS {
			m.lastTS = msg.lastTS
		}
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
		m.vp.SetContent(dimStyle.Render("No events yet..."))
		return
	}
	m.vp.SetContent(strings.Join(m.lines, "\n"))
	if m.autoScroll {
		m.vp.GotoBottom()
	}
}

// fetchEventsCmd returns a Cmd that fetches events from the daemon RPC.
// Uses lastTS for incremental reads so we only get new events.
func (m LogStreamModel) fetchEventsCmd() tea.Cmd {
	agentID := m.agentID
	c := m.client
	afterTS := m.lastTS
	return func() tea.Msg {
		result, err := c.EventsList(agentID, afterTS)
		if err != nil {
			return logEventsMsg{err: err}
		}
		return logEventsMsg{lines: result.Lines, lastTS: result.LastTS}
	}
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
