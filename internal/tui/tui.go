// Package tui implements the interactive terminal dashboard for monitoring
// the aetherflow daemon. It provides a k9s/btop-style interface with a
// dashboard overview, agent detail panels, and log streaming.
//
// The TUI communicates with the daemon via the existing Unix socket RPC
// protocol and polls for updates on a configurable interval.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles are defined at package level so they're allocated once, not on
// every View() call. As panes and screens are added, new styles go here.
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")) // bright blue

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))
)

// Config holds the configuration needed to run the TUI.
type Config struct {
	// SocketPath is the Unix socket path for the daemon RPC.
	SocketPath string
}

// Model is the top-level bubbletea model for the TUI.
type Model struct {
	config Config
	width  int
	height int
}

// New creates a new TUI model with the given configuration.
func New(cfg Config) Model {
	return Model{
		config: cfg,
	}
}

// Init implements tea.Model. No initial commands needed for the skeleton.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model. Handles key presses and window resize.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

// View implements tea.Model. Renders the current screen.
func (m Model) View() string {
	title := titleStyle.Render("aetherflow")
	socket := dimStyle.Render(fmt.Sprintf("socket: %s", m.config.SocketPath))
	quit := dimStyle.Render("press q to quit")

	return fmt.Sprintf("\n  %s\n\n  %s\n\n  %s\n", title, socket, quit)
}

// Run starts the TUI program with alternate screen buffer.
func Run(cfg Config) error {
	m := New(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
