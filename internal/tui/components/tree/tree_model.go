package tree

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/tui/core"
)

// Model wraps StackTreeRenderer to make it a tea.Model for the storyboard
type Model struct {
	Renderer *StackTreeRenderer
	Options  RenderOptions
	Width    int
	Height   int
}

// NewModel creates a new Model with the given renderer.
func NewModel(renderer *StackTreeRenderer) *Model {
	return &Model{
		Renderer: renderer,
		Options: RenderOptions{
			Mode: RenderModeFull,
		},
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update updates the model based on the message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "s":
			if m.Options.Mode == RenderModeFull {
				m.Options.Mode = RenderModeCompact
			} else {
				m.Options.Mode = RenderModeFull
			}
			return m, nil
		case core.KeyQuit, core.KeyCtrlC, core.KeyEsc:
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
	}
	return m, nil
}

// View returns the string representation of the model.
func (m Model) View() tea.View {
	lines := m.Renderer.RenderStack(m.Renderer.trunk, m.Options)
	content := strings.Join(lines, "\n")

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
	help := helpStyle.Render("s: toggle short | q: back")

	return tea.NewView(content + "\n" + help)
}
