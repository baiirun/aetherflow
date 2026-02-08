// Package tui implements the interactive terminal dashboard for monitoring
// the aetherflow daemon. It provides a k9s/btop-style interface with a
// dashboard overview, agent detail panels, and log streaming.
//
// The TUI communicates with the daemon via the existing Unix socket RPC
// protocol and polls for updates on a configurable interval.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/geobrowser/aetherflow/internal/client"
)

// pollInterval is the default interval between daemon status polls.
const pollInterval = 2 * time.Second

// Styles are defined at package level so they're allocated once, not on
// every View() call. As panes and screens are added, new styles go here.
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")) // bright blue

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	greenStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10"))

	yellowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("11"))

	redStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9"))

	cyanStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14"))

	blueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12"))

	magentaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("13"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("237")) // subtle highlight

	paneHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("14"))

	paneBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)

	paneBorderSelected = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("14")).
				Padding(0, 1)
)

// Config holds the configuration needed to run the TUI.
type Config struct {
	// SocketPath is the Unix socket path for the daemon RPC.
	SocketPath string
}

// statusMsg carries the result of a daemon status poll.
type statusMsg struct {
	status *client.FullStatus
	err    error
}

// agentDetailsMsg carries the result of polling all agents' details.
type agentDetailsMsg struct {
	details map[string]*client.AgentDetail
}

// tickMsg triggers the next poll cycle.
type tickMsg time.Time

// Model is the top-level bubbletea model for the TUI.
type Model struct {
	config       Config
	client       *client.Client
	width        int
	height       int
	status       *client.FullStatus
	err          error
	selected     int                            // index of selected agent pane
	agentDetails map[string]*client.AgentDetail // agentID → detail with tool calls
}

// New creates a new TUI model with the given configuration.
func New(cfg Config) Model {
	return Model{
		config: cfg,
		client: client.New(cfg.SocketPath),
	}
}

// Init implements tea.Model. Kicks off the first status poll and tick.
// Agent details for all running agents are fetched once the first
// statusMsg arrives.
func (m Model) Init() tea.Cmd {
	return tea.Batch(pollStatus(m.client), tick())
}

// pollStatus fetches the full daemon status as a bubbletea Cmd.
func pollStatus(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		status, err := c.StatusFull()
		return statusMsg{status: status, err: err}
	}
}

// pollAgentDetails fetches detail for all running agents. Each agent gets
// its own RPC call; results are collected into a single message.
func pollAgentDetails(c *client.Client, agents []client.AgentStatus) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}
	return func() tea.Msg {
		details := make(map[string]*client.AgentDetail, len(agents))
		for _, a := range agents {
			detail, err := c.StatusAgent(a.ID, 5)
			if err == nil {
				details[a.ID] = detail
			}
		}
		return agentDetailsMsg{details: details}
	}
}

// tick returns a Cmd that fires a tickMsg after the poll interval.
func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update implements tea.Model. Handles key presses, window resize, and status polls.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.status != nil && len(m.status.Agents) > 0 {
				m.selected = min(m.selected+1, len(m.status.Agents)-1)
			}
		case "k", "up":
			if m.selected > 0 {
				m.selected--
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case statusMsg:
		m.status = msg.status
		m.err = msg.err
		// Clamp selection if agents list shrank.
		if m.status != nil && m.selected >= len(m.status.Agents) {
			m.selected = max(0, len(m.status.Agents)-1)
		}
		// Fetch details for all agents on first status arrival.
		if m.agentDetails == nil && m.status != nil {
			return m, pollAgentDetails(m.client, m.status.Agents)
		}

	case agentDetailsMsg:
		m.agentDetails = msg.details

	case tickMsg:
		cmds := []tea.Cmd{pollStatus(m.client), tick()}
		if m.status != nil && len(m.status.Agents) > 0 {
			cmds = append(cmds, pollAgentDetails(m.client, m.status.Agents))
		}
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

// View implements tea.Model. Renders the dashboard screen.
func (m Model) View() string {
	var b strings.Builder

	b.WriteString(m.viewHeader())
	b.WriteString("\n")
	b.WriteString(m.viewAgentPanes())
	b.WriteString(m.viewFooter())

	return b.String()
}

// viewHeader renders the top bar with pool stats, mode, and project.
func (m Model) viewHeader() string {
	if m.err != nil {
		return fmt.Sprintf("\n  %s  %s  %s\n",
			titleStyle.Render("aetherflow"),
			redStyle.Render("disconnected"),
			dimStyle.Render(m.err.Error()),
		)
	}

	if m.status == nil {
		return fmt.Sprintf("\n  %s  %s\n",
			titleStyle.Render("aetherflow"),
			dimStyle.Render("connecting..."),
		)
	}

	s := m.status
	active := len(s.Agents)

	var util string
	if active > 0 {
		util = greenStyle.Render(fmt.Sprintf("%d/%d active", active, s.PoolSize))
	} else {
		util = dimStyle.Render(fmt.Sprintf("%d/%d active", active, s.PoolSize))
	}

	mode := ""
	switch s.PoolMode {
	case "draining":
		mode = "  " + yellowStyle.Render("[draining]")
	case "paused":
		mode = "  " + redStyle.Render("[paused]")
	}

	project := ""
	if s.Project != "" {
		project = "  " + dimStyle.Render("("+s.Project+")")
	}

	return fmt.Sprintf("\n  %s  %s%s%s\n",
		titleStyle.Render("aetherflow"),
		util, mode, project,
	)
}

// viewAgentPanes renders a stacked pane for every running agent. Each pane
// has a header with agent metadata and a list of recent tool calls.
func (m Model) viewAgentPanes() string {
	if m.status == nil || m.err != nil {
		return ""
	}

	agents := m.status.Agents
	if len(agents) == 0 {
		return "  " + dimStyle.Render("No agents running") + "\n\n"
	}

	var b strings.Builder

	for i, a := range agents {
		b.WriteString(m.viewOnePane(i, a))
	}

	idle := m.status.PoolSize - len(agents)
	if idle > 0 {
		b.WriteString(fmt.Sprintf("  %s\n", dimStyle.Render(fmt.Sprintf("+ %d idle", idle))))
	}

	return b.String()
}

// viewOnePane renders a single agent pane with a border. The pane contains
// a header line with agent metadata and rows of recent tool calls.
// Selected pane gets a bright cyan border; others get a dim border.
func (m Model) viewOnePane(index int, a client.AgentStatus) string {
	// Border consumes 2 columns (1 each side), padding consumes 2 more
	// (Padding(0,1) = 1 each side). Content area is width - 6.
	// Default to 80 if we haven't received a WindowSizeMsg yet.
	w := m.width
	if w == 0 {
		w = 80
	}
	innerWidth := max(20, w-6)
	// boxWidth is what lipgloss Width() gets — excludes borders but includes padding.
	boxWidth := innerWidth + 2

	var b strings.Builder

	// Header: left = "name  task title…"  right = "uptime  role" (right-justified)
	uptime := formatUptime(a.SpawnTime)
	rightText := uptime + "  " + a.Role
	rightLen := len([]rune(rightText))

	// Build left side and track visible width.
	leftLen := len([]rune(a.ID)) + 2 + len([]rune(a.TaskID)) // "name  taskid"
	titleText := ""
	if a.TaskTitle != "" {
		// Budget: innerWidth - leftLen - " " - min_gap(2) - rightLen
		budget := innerWidth - leftLen - 1 - 2 - rightLen
		if budget > 5 {
			titleText = truncate(a.TaskTitle, budget)
			leftLen += 1 + len([]rune(titleText))
		}
	}

	gap := max(2, innerWidth-leftLen-rightLen)

	// Assemble with styles.
	var header strings.Builder
	header.WriteString(paneHeaderStyle.Render(a.ID))
	header.WriteString("  ")
	header.WriteString(blueStyle.Render(a.TaskID))
	if titleText != "" {
		header.WriteString(" ")
		header.WriteString(dimStyle.Render(titleText))
	}
	header.WriteString(strings.Repeat(" ", gap))
	header.WriteString(greenStyle.Render(uptime))
	header.WriteString("  ")
	header.WriteString(magentaStyle.Render(a.Role))

	b.WriteString(header.String())

	// Tool call rows.
	const (
		colTime = 8
		colTool = 10
		colDur  = 7
	)

	// Budget: colTime + "  " + colTool + " " + titleMax + " " + colDur
	titleMax := innerWidth - colTime - 2 - colTool - 1 - 1 - colDur
	if titleMax < 10 {
		titleMax = 10
	}

	// Column headers.
	b.WriteString(fmt.Sprintf("\n%s  %s %s %s",
		dimStyle.Render(padLeft("AGE", colTime)),
		dimStyle.Render(padRight("TOOL", colTool)),
		dimStyle.Render(padRight("INPUT", titleMax)),
		dimStyle.Render(padLeft("DUR", colDur)),
	))

	detail, hasDetail := m.agentDetails[a.ID]
	if !hasDetail || len(detail.ToolCalls) == 0 {
		b.WriteString("\n" + dimStyle.Render("waiting for tool calls..."))
	} else {
		// Tool calls arrive oldest-first; iterate in reverse for
		// most-recent-at-top.
		for i := len(detail.ToolCalls) - 1; i >= 0; i-- {
			tc := detail.ToolCalls[i]
			age := formatRelativeTime(tc.Timestamp)

			label := tc.Input
			if tc.Title != "" {
				label = tc.Title
			}
			label = truncate(label, titleMax)

			dur := "—"
			if tc.DurationMs > 0 {
				dur = fmt.Sprintf("%.1fs", float64(tc.DurationMs)/1000)
			}

			b.WriteString(fmt.Sprintf("\n%s  %s %s %s",
				dimStyle.Render(padLeft(age, colTime)),
				cyanStyle.Render(padRight(tc.Tool, colTool)),
				padRight(label, titleMax),
				dimStyle.Render(padLeft(dur, colDur)),
			))
		}
	}

	content := b.String()

	border := paneBorder.Width(boxWidth)
	if index == m.selected {
		border = paneBorderSelected.Width(boxWidth)
	}

	return border.Render(content) + "\n"
}

// viewFooter renders the bottom help line.
func (m Model) viewFooter() string {
	return "  " + dimStyle.Render("j/k navigate  enter select  q quit") + "\n"
}

// formatRelativeTime returns a human-readable relative time string.
func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	if d < 0 {
		return "now"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh ago", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

// formatUptime returns a human-readable duration since the given time.
func formatUptime(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%dh", days, h)
	}
}

// truncate shortens s to max runes, appending an ellipsis if truncated.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// padRight pads s with spaces to width. If s is longer, it's truncated.
func padRight(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

// padLeft pads s with leading spaces to width. If s is longer, it's truncated.
func padLeft(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return strings.Repeat(" ", width-len(runes)) + s
}

// Run starts the TUI program with alternate screen buffer.
func Run(cfg Config) error {
	m := New(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
