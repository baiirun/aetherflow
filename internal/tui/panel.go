package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/geobrowser/aetherflow/internal/client"
)

// taskDetailMsg carries the result of fetching task detail from prog.
type taskDetailMsg struct {
	detail *TaskDetail
	err    error
}

// panelAgentDetailMsg carries the result of fetching agent detail for the panel.
type panelAgentDetailMsg struct {
	detail *client.AgentDetail
	err    error
}

// fetchTaskDetailCmd returns a bubbletea Cmd that fetches task detail.
func fetchTaskDetailCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		detail, err := fetchTaskDetail(taskID)
		return taskDetailMsg{detail: detail, err: err}
	}
}

// fetchPanelAgentDetailCmd fetches agent detail for the panel view.
func fetchPanelAgentDetailCmd(c *client.Client, agentID string) tea.Cmd {
	return func() tea.Msg {
		detail, err := c.StatusAgent(agentID, 20)
		return panelAgentDetailMsg{detail: detail, err: err}
	}
}

// paneID identifies which pane has focus in the panel.
type paneID int

const (
	paneTaskInfo paneID = iota
	paneToolCalls
	paneProgLogs
	paneCount // sentinel for cycling
)

// PanelModel holds the state for the agent master panel screen.
type PanelModel struct {
	agent       client.AgentStatus
	agentDetail *client.AgentDetail
	taskDetail  *TaskDetail
	taskErr     error

	taskVP viewport.Model // scrollable task info pane
	logsVP viewport.Model // scrollable prog logs pane
	focus  paneID

	ready  bool
	width  int
	height int
}

// NewPanelModel creates a new panel for the given agent.
func NewPanelModel(agent client.AgentStatus, width, height int) PanelModel {
	m := PanelModel{
		agent:  agent,
		width:  width,
		height: height,
		focus:  paneToolCalls,
	}
	if width > 0 && height > 0 {
		m.initViewports()
	}
	return m
}

// Pane box anatomy (with paneBorder style: Border + Padding(0,1)):
//
//   ╭──────────────────────────╮  ← border top (1 row)
//   │ content area here        │  ← border left(1) + pad left(1) + text + pad right(1) + border right(1)
//   ╰──────────────────────────╯  ← border bottom (1 row)
//
// lipgloss Width(W) sets the width INSIDE the border but INCLUDING padding.
// So: rendered_width = border_left(1) + W + border_right(1) = W + 2
//     text_area      = W - pad_left(1) - pad_right(1)       = W - 2
//
// lipgloss Height(H) sets the height INSIDE the border (content rows).
// So: rendered_height = border_top(1) + H + border_bottom(1) = H + 2

const (
	borderLR   = 2 // left + right border chars
	paddingLR  = 2 // Padding(0,1) → 1 left + 1 right
	borderTB   = 2 // top + bottom border rows
	metaLines  = 5 // "Agent" header + name + pid + role/up + spawned
	headerRows = 3 // panel header bar + blank line
	footerRows = 2 // blank line + help text
)

type panelLayout struct {
	// Text area widths (what content should be sized to).
	leftTextW  int
	rightTextW int
	// lipgloss Width() values (inside border, including padding).
	leftBoxW  int
	rightBoxW int
	// Heights.
	bodyH     int // total rows available for pane area
	metaBoxH  int // lipgloss Height for meta (content rows inside border)
	toolsBoxH int // lipgloss Height for tools
	logsBoxH  int // lipgloss Height for logs
	taskBoxH  int // lipgloss Height for task info
}

func calcLayout(termW, termH int) panelLayout {
	bodyH := max(10, termH-headerRows-footerRows)

	// Total rendered width of two columns + 1-col gap:
	//   (leftBoxW + borderLR) + 1 + (rightBoxW + borderLR) = termW
	//   leftBoxW + rightBoxW = termW - borderLR - 1 - borderLR = termW - 5
	boxBudget := max(20, termW-5)
	leftBoxW := boxBudget / 2
	rightBoxW := boxBudget - leftBoxW

	leftTextW := max(10, leftBoxW-paddingLR)
	rightTextW := max(10, rightBoxW-paddingLR)

	// Vertical layout for right column.
	// Each pane's rendered height = boxH + borderTB.
	// meta + tools + logs rendered heights must sum to bodyH:
	//   (metaBoxH + 2) + (toolsBoxH + 2) + (logsBoxH + 2) = bodyH
	//   metaBoxH + toolsBoxH + logsBoxH = bodyH - 6
	innerBudget := max(6, bodyH-3*borderTB)
	metaBoxH := metaLines
	remaining := max(4, innerBudget-metaBoxH)
	toolsBoxH := remaining * 55 / 100
	logsBoxH := remaining - toolsBoxH

	// Left column: single pane fills entire body.
	// rendered height = taskBoxH + borderTB = bodyH
	taskBoxH := max(4, bodyH-borderTB)

	return panelLayout{
		leftTextW: leftTextW, rightTextW: rightTextW,
		leftBoxW: leftBoxW, rightBoxW: rightBoxW,
		bodyH:    bodyH,
		metaBoxH: metaBoxH, toolsBoxH: toolsBoxH, logsBoxH: logsBoxH,
		taskBoxH: taskBoxH,
	}
}

func (m *PanelModel) initViewports() {
	l := calcLayout(m.width, m.height)

	m.taskVP = viewport.New(l.rightTextW, l.taskBoxH)
	m.taskVP.SetContent(renderTaskInfo(m.taskDetail, l.rightTextW))

	m.logsVP = viewport.New(l.leftTextW, l.logsBoxH)
	m.logsVP.SetContent(m.renderProgLogs(l.leftTextW))

	m.ready = true
}

// renderProgLogs formats prog log entries for the logs viewport.
func (m *PanelModel) renderProgLogs(textW int) string {
	if m.taskDetail == nil {
		return paneHeaderStyle.Render("Prog Logs") + "\n" + dimStyle.Render("Loading...")
	}
	logs := m.taskDetail.Logs
	if len(logs) == 0 {
		return paneHeaderStyle.Render("Prog Logs") + "\n" + dimStyle.Render("No logs yet")
	}

	var b strings.Builder
	b.WriteString(paneHeaderStyle.Render(fmt.Sprintf("Prog Logs (%d)", len(logs))))
	msgW := max(10, textW-18) // 16 for timestamp + 2 for gap
	for _, log := range logs {
		b.WriteString("\n")
		ts := log.CreatedAt
		if len(ts) > 16 {
			ts = ts[:16]
		}
		b.WriteString(fmt.Sprintf("%s  %s", dimStyle.Render(ts), wrapText(log.Message, msgW)))
	}
	return b.String()
}

// renderToolCalls formats tool calls, limited to maxRows content lines.
func (m *PanelModel) renderToolCalls(textW, maxRows int) string {
	var b strings.Builder
	b.WriteString(paneHeaderStyle.Render("Tool Calls"))

	if m.agentDetail == nil || len(m.agentDetail.ToolCalls) == 0 {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("waiting for tool calls..."))
		return b.String()
	}

	const (
		colTime = 8
		colTool = 10
		colDur  = 7
		gaps    = 4 // "  " + " " + " " between the 4 columns
	)
	inputW := max(5, textW-colTime-colTool-colDur-gaps)

	// Column headers use 1 row.
	b.WriteString(fmt.Sprintf("\n%s  %s %s %s",
		dimStyle.Render(padLeft("AGE", colTime)),
		dimStyle.Render(padRight("TOOL", colTool)),
		dimStyle.Render(padRight("INPUT", inputW)),
		dimStyle.Render(padLeft("DUR", colDur)),
	))

	// Data rows. Header title + column labels = 2 lines.
	rowBudget := max(1, maxRows-2)
	calls := m.agentDetail.ToolCalls
	start := max(0, len(calls)-rowBudget)

	for i := len(calls) - 1; i >= start; i-- {
		tc := calls[i]
		age := formatRelativeTime(tc.Timestamp)

		label := tc.Input
		if tc.Title != "" {
			label = tc.Title
		}
		label = truncate(label, inputW)

		dur := "—"
		if tc.DurationMs > 0 {
			dur = formatDuration(tc.DurationMs)
		}

		b.WriteString(fmt.Sprintf("\n%s  %s %s %s",
			dimStyle.Render(padLeft(age, colTime)),
			cyanStyle.Render(padRight(tc.Tool, colTool)),
			padRight(label, inputW),
			dimStyle.Render(padLeft(dur, colDur)),
		))
	}

	return b.String()
}

// formatDuration renders milliseconds as a human-readable duration.
func formatDuration(ms int) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	s := float64(ms) / 1000
	if s < 60 {
		return fmt.Sprintf("%.1fs", s)
	}
	m := int(s) / 60
	rs := int(s) % 60
	return fmt.Sprintf("%dm%ds", m, rs)
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
		m.initViewports()

	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			m.focus = (m.focus + 1) % paneCount
			return m, nil
		case "shift+tab":
			m.focus = (m.focus + paneCount - 1) % paneCount
			return m, nil
		}

	case taskDetailMsg:
		firstLoad := m.taskDetail == nil
		m.taskDetail = msg.detail
		m.taskErr = msg.err
		if m.ready {
			m.taskVP.SetContent(renderTaskInfo(m.taskDetail, m.taskVP.Width))
			if firstLoad {
				m.taskVP.GotoTop()
			}
			m.logsVP.SetContent(m.renderProgLogs(m.logsVP.Width))
			if firstLoad {
				m.logsVP.GotoBottom()
			}
		}

	case panelAgentDetailMsg:
		if msg.err == nil && msg.detail != nil {
			m.agentDetail = msg.detail
			m.agent = msg.detail.AgentStatus
		}
	}

	// Forward scroll keys to the focused viewport.
	if m.ready {
		var cmd tea.Cmd
		switch m.focus {
		case paneTaskInfo:
			m.taskVP, cmd = m.taskVP.Update(msg)
		case paneProgLogs:
			m.logsVP, cmd = m.logsVP.Update(msg)
		}
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
		b.WriteString(m.viewBody())
	}
	b.WriteString(m.viewPanelFooter())
	return b.String()
}

// viewBody renders the two-column pane layout.
// Left: agent meta + tool calls + prog logs. Right: task info.
func (m PanelModel) viewBody() string {
	l := calcLayout(m.width, m.height)

	// Left column: meta + tools + logs stacked vertically.
	meta := m.boxStyle(-1, l.leftBoxW, l.metaBoxH).
		Render(m.renderAgentMeta())

	tools := m.boxStyle(paneToolCalls, l.leftBoxW, l.toolsBoxH).
		Render(m.renderToolCalls(l.leftTextW, l.toolsBoxH))

	logs := m.boxStyle(paneProgLogs, l.leftBoxW, l.logsBoxH).
		Render(m.logsVP.View())

	left := lipgloss.JoinVertical(lipgloss.Left, meta, tools, logs)

	// Right column: task info (full body height).
	right := m.boxStyle(paneTaskInfo, l.rightBoxW, l.taskBoxH).
		Render(m.taskVP.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right) + "\n"
}

// boxStyle returns a bordered lipgloss style.
// boxW = lipgloss Width (inside border, includes padding).
// boxH = lipgloss Height (inside border, content rows).
func (m PanelModel) boxStyle(id paneID, boxW, boxH int) lipgloss.Style {
	base := paneBorder
	if id == m.focus {
		base = paneBorderSelected
	}
	return base.Width(boxW).Height(boxH)
}

// renderAgentMeta renders the compact agent metadata.
func (m PanelModel) renderAgentMeta() string {
	a := m.agent
	uptime := formatUptime(a.SpawnTime)
	spawnStr := "—"
	if !a.SpawnTime.IsZero() {
		spawnStr = a.SpawnTime.Format("15:04:05")
	}

	var b strings.Builder
	b.WriteString(paneHeaderStyle.Render("Agent") + "\n")
	b.WriteString(fmt.Sprintf("%s %s\n", dimStyle.Render("Name:"), a.ID))
	b.WriteString(fmt.Sprintf("%s %d\n", dimStyle.Render("PID:"), a.PID))
	b.WriteString(fmt.Sprintf("%s %s  %s %s\n",
		dimStyle.Render("Role:"), magentaStyle.Render(a.Role),
		dimStyle.Render("Up:"), greenStyle.Render(uptime),
	))
	b.WriteString(fmt.Sprintf("%s %s", dimStyle.Render("Spawned:"), spawnStr))
	return b.String()
}

// viewPanelHeader renders the top bar.
func (m PanelModel) viewPanelHeader() string {
	return fmt.Sprintf("\n  %s  %s  %s  %s  %s\n",
		titleStyle.Render("aetherflow"),
		paneHeaderStyle.Render(m.agent.ID),
		blueStyle.Render(m.agent.TaskID),
		greenStyle.Render(formatUptime(m.agent.SpawnTime)),
		magentaStyle.Render(m.agent.Role),
	)
}

// viewPanelFooter renders the bottom help line.
func (m PanelModel) viewPanelFooter() string {
	focusLabel := ""
	switch m.focus {
	case paneTaskInfo:
		focusLabel = "task info"
	case paneToolCalls:
		focusLabel = "tool calls"
	case paneProgLogs:
		focusLabel = "prog logs"
	}

	scrollPct := ""
	switch m.focus {
	case paneTaskInfo:
		if m.ready {
			scrollPct = dimStyle.Render(fmt.Sprintf("  %.0f%%", m.taskVP.ScrollPercent()*100))
		}
	case paneProgLogs:
		if m.ready {
			scrollPct = dimStyle.Render(fmt.Sprintf("  %.0f%%", m.logsVP.ScrollPercent()*100))
		}
	}

	return fmt.Sprintf("  %s  %s%s\n",
		dimStyle.Render("j/k scroll  tab focus  l logs  q back"),
		cyanStyle.Render(focusLabel),
		scrollPct,
	)
}
