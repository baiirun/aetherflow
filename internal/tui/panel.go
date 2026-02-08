package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/geobrowser/aetherflow/internal/client"
)

// taskDetailMsg carries the result of fetching task detail from prog.
type taskDetailMsg struct {
	detail *TaskDetail
	err    error
}

// fetchTaskDetailCmd returns a bubbletea Cmd that fetches task detail.
func fetchTaskDetailCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		detail, err := fetchTaskDetail(taskID)
		return taskDetailMsg{detail: detail, err: err}
	}
}

// PanelModel holds the state for the agent master panel screen.
type PanelModel struct {
	agent      client.AgentStatus
	taskDetail *TaskDetail
	taskErr    error
	viewport   viewport.Model
	ready      bool // true once we've received a WindowSizeMsg and can init the viewport
	width      int
	height     int
}

// NewPanelModel creates a new panel for the given agent.
func NewPanelModel(agent client.AgentStatus, width, height int) PanelModel {
	m := PanelModel{
		agent:  agent,
		width:  width,
		height: height,
	}

	// Initialize viewport if we already have dimensions.
	if width > 0 && height > 0 {
		m.initViewport()
	}

	return m
}

// initViewport sets up the viewport with current dimensions.
// Reserves space for the panel header (3 lines) and footer (2 lines).
func (m *PanelModel) initViewport() {
	headerHeight := 3 // header bar + blank line
	footerHeight := 2 // blank line + help text

	vpWidth := max(20, m.width-4) // some horizontal padding
	vpHeight := max(5, m.height-headerHeight-footerHeight)

	m.viewport = viewport.New(vpWidth, vpHeight)
	m.viewport.SetContent(m.renderContent())
	m.ready = true
}

// renderContent builds the full scrollable content for the viewport.
func (m *PanelModel) renderContent() string {
	contentWidth := max(20, m.width-6)
	return renderTaskInfo(m.taskDetail, contentWidth)
}

// Init returns the command to fetch task detail.
func (m PanelModel) Init() tea.Cmd {
	return fetchTaskDetailCmd(m.agent.TaskID)
}

// Update handles messages for the panel screen.
func (m PanelModel) Update(msg tea.Msg) (PanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.initViewport()

	case taskDetailMsg:
		m.taskDetail = msg.detail
		m.taskErr = msg.err
		if m.ready {
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
		}
	}

	// Forward remaining messages to viewport for scroll handling.
	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the agent master panel.
func (m PanelModel) View() string {
	var b strings.Builder

	b.WriteString(m.viewPanelHeader())
	b.WriteString("\n")

	if !m.ready {
		b.WriteString("  " + dimStyle.Render("Loading...") + "\n")
	} else {
		b.WriteString(m.viewport.View())
		b.WriteString("\n")
	}

	b.WriteString(m.viewPanelFooter())

	return b.String()
}

// viewPanelHeader renders the top bar showing which agent we're viewing.
func (m PanelModel) viewPanelHeader() string {
	uptime := formatUptime(m.agent.SpawnTime)

	return fmt.Sprintf("\n  %s  %s  %s  %s  %s\n",
		titleStyle.Render("aetherflow"),
		paneHeaderStyle.Render(m.agent.ID),
		blueStyle.Render(m.agent.TaskID),
		greenStyle.Render(uptime),
		magentaStyle.Render(m.agent.Role),
	)
}

// viewPanelFooter renders the bottom help line.
func (m PanelModel) viewPanelFooter() string {
	scroll := ""
	if m.ready {
		pct := m.viewport.ScrollPercent() * 100
		scroll = dimStyle.Render(fmt.Sprintf("  %.0f%%", pct))
	}

	return fmt.Sprintf("  %s%s\n",
		dimStyle.Render("j/k scroll  q back"),
		scroll,
	)
}

// handlePanelKey processes key events for the panel. Returns true if
// the key means "go back to dashboard".
func handlePanelKey(key string) bool {
	return key == "q" || key == "esc"
}
