package tui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/tui/keys"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Tree symbols for rendering
const (
	branchSymbol = "◯"
	verticalLine = "│"
	branchArrow  = "▸"
)

// reorderModel is the bubbletea model for reordering branches
type reorderModel struct {
	branches      []string
	originalOrder []string // tracks original positions to detect moves
	trunk         string   // trunk branch name (displayed but not selectable)
	cursor        int
	confirmed     bool
	canceled      bool
	keys          keys.ReorderKeyMap
}

// newReorderModel creates a new reorder TUI model
func newReorderModel(branches []string, trunk string) reorderModel {
	// Store original order to track movements
	original := make([]string, len(branches))
	copy(original, branches)

	return reorderModel{
		branches:      branches,
		originalOrder: original,
		trunk:         trunk,
		cursor:        0,
		keys:          keys.DefaultReorder,
	}
}

func (m reorderModel) Init() tea.Cmd {
	return nil
}

func (m reorderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Cancel):
			m.canceled = true
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if len(m.branches) > 0 {
				if m.cursor > 0 {
					m.cursor--
				} else {
					m.cursor = len(m.branches) - 1 // Wrap to last
				}
			}

		case key.Matches(msg, m.keys.Down):
			if len(m.branches) > 0 {
				if m.cursor < len(m.branches)-1 {
					m.cursor++
				} else {
					m.cursor = 0 // Wrap to first
				}
			}

		case key.Matches(msg, m.keys.MoveUp):
			if m.cursor > 0 {
				// Swap with previous
				m.branches[m.cursor], m.branches[m.cursor-1] = m.branches[m.cursor-1], m.branches[m.cursor]
				m.cursor--
			}

		case key.Matches(msg, m.keys.MoveDown):
			if m.cursor < len(m.branches)-1 {
				// Swap with next
				m.branches[m.cursor], m.branches[m.cursor+1] = m.branches[m.cursor+1], m.branches[m.cursor]
				m.cursor++
			}

		case key.Matches(msg, m.keys.Select):
			m.confirmed = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// isBranchMoved checks if a branch has moved from its original position
func (m reorderModel) isBranchMoved(branch string) bool {
	currentPos := -1
	originalPos := -1

	for i, b := range m.branches {
		if b == branch {
			currentPos = i
			break
		}
	}

	for i, b := range m.originalOrder {
		if b == branch {
			originalPos = i
			break
		}
	}

	return currentPos != originalPos
}

// branchNeedsRestack checks if a branch should show the restack suggestion style.
// A branch has restack suggested if it has moved OR any branch below it (toward trunk) has moved
// Since branches are displayed tip-first, "below" means higher index
func (m reorderModel) branchNeedsRestack(branchIndex int) bool {
	// Check if this branch or any branch below it (higher index, closer to trunk) has moved
	for i := branchIndex; i < len(m.branches); i++ {
		if m.isBranchMoved(m.branches[i]) {
			return true
		}
	}
	return false
}

func (m reorderModel) View() tea.View {
	var b strings.Builder

	// Header with title and help
	title := "Reorder Branches"
	help := "'↑/k' '↓/j' navigate, 'K' 'J' move, 'enter' confirm, 'esc' cancel"
	header := style.ColorDim(fmt.Sprintf(" %s | %d branches | %s", title, len(m.branches), help))
	b.WriteString(header)
	b.WriteString("\n\n")

	// Styles
	movedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))  // Blue for moved
	restackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // Green for restack suggestions
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))  // Default/white
	trunkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")) // Pink for trunk
	dimStyle := lipgloss.NewStyle().Foreground(style.DimColor())        // Dim for tree lines
	selectionBg := lipgloss.NewStyle().Background(lipgloss.Color("6")).Foreground(lipgloss.Color("0")).Bold(true)

	// Render branch tree (tip-first, trunk at bottom)
	for i, branch := range m.branches {
		isSelected := i == m.cursor
		isMoved := m.isBranchMoved(branch)
		needsRestack := m.branchNeedsRestack(i)

		// Determine branch style based on state
		var branchStyle lipgloss.Style
		switch {
		case isMoved:
			branchStyle = movedStyle
		case needsRestack:
			branchStyle = restackStyle
		default:
			branchStyle = normalStyle
		}

		// Build the line: symbol + arrow + branch name
		symbol := branchSymbol
		arrow := branchArrow

		if isSelected {
			// Selected row: use selection background
			symbolStyled := selectionBg.Render(symbol)
			arrowStyled := selectionBg.Render(arrow)
			branchStyled := selectionBg.Render(branch)
			fmt.Fprintf(&b, "%s%s%s\n", symbolStyled, arrowStyled, branchStyled)
		} else {
			// Non-selected row: apply branch-specific color
			symbolStyled := branchStyle.Render(symbol)
			arrowStyled := branchStyle.Render(arrow)
			branchStyled := branchStyle.Render(branch)
			fmt.Fprintf(&b, "%s%s%s\n", symbolStyled, arrowStyled, branchStyled)
		}

		// Add vertical connector line (except after the last branch before trunk)
		if i < len(m.branches)-1 || m.trunk != "" {
			b.WriteString(dimStyle.Render(verticalLine) + "\n")
		}
	}

	// Show trunk at the bottom (not selectable)
	if m.trunk != "" {
		symbol := branchSymbol
		arrow := branchArrow
		fmt.Fprintf(&b, "%s%s%s\n",
			trunkStyle.Render(symbol),
			trunkStyle.Render(arrow),
			trunkStyle.Render(m.trunk))
	}

	return tea.NewView(b.String())
}

// NewReorderModel creates a tea.Model for a reorder prompt (used by stories/demo)
func NewReorderModel(branches []string, trunk string) tea.Model {
	return newReorderModel(branches, trunk)
}

// RunReorderTUI runs the reorder TUI and returns the new order
// trunk is displayed at the bottom but is not selectable or reorderable
func RunReorderTUI(branches []string, trunk string) ([]string, error) {
	if !IsTTY() {
		return nil, fmt.Errorf("RunReorderTUI called in non-interactive mode")
	}
	m := newReorderModel(branches, trunk)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))

	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	res := finalModel.(reorderModel)
	if res.canceled {
		return nil, fmt.Errorf("reorder canceled")
	}

	return res.branches, nil
}
