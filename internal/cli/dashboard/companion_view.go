package dashboard

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/getstackit/stackit/internal/shippable"
	"github.com/getstackit/stackit/internal/tui/components/tree"
)

// View renders the companion as a single narrow column so it remains useful
// beside an editor or agent pane.
func (m *companionModel) View() tea.View {
	width := max(m.Width, 40)
	lines := []string{m.renderWorkingTreeStrip()}

	if m.loading {
		lines = append(lines, ds.HeaderStatus.Render("refreshing local stack state…"))
	}
	if m.errorMessage != "" {
		lines = append(lines, ds.ErrorText.Render(m.errorMessage))
	}
	if m.analysis != nil {
		lines = append(lines, m.renderShippableRollup()...)
		lines = append(lines, m.renderStackTree(width)...)
	}
	lines = append(lines, ds.Footer.Render("s ship  •  q quit"))

	for i, line := range lines {
		lines[i] = truncateCompanionLine(line, width)
	}

	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

func (m *companionModel) renderWorkingTreeStrip() string {
	branch := "detached"
	if m.engine != nil && m.engine.CurrentBranchName() != "" {
		branch = m.engine.CurrentBranchName()
	}
	return fmt.Sprintf("branch %s  |  staged %d  unstaged %d  untracked %d",
		branch, m.workingTree.staged, m.workingTree.unstaged, m.workingTree.untracked)
}

func (m *companionModel) renderShippableRollup() []string {
	readyBranches := 0
	for _, stack := range m.analysis.Stacks {
		if stack.Status == shippable.StatusShippable {
			readyBranches += stack.BranchCount()
		}
	}

	lines := make([]string, 1, 1+len(m.analysis.Stacks))
	lines[0] = ds.PaneHeader.Render(fmt.Sprintf("%d branches ready to ship", readyBranches))
	currentStackRoot := m.currentStackRoot()
	for _, stack := range m.analysis.Stacks {
		line := fmt.Sprintf("%s %s", companionStatusIcon(stack.Status), stack.RootBranch())
		if stack.RootBranch() == currentStackRoot {
			line = ds.SelectedRow.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

func (m *companionModel) renderStackTree(width int) []string {
	if m.renderer == nil || m.analysis == nil || len(m.analysis.Stacks) == 0 {
		return []string{ds.PaneHeader.Render("stack tree"), ds.HeaderStatus.Render("no tracked stacks")}
	}

	treeLines := m.renderer.RenderStack(m.engine.Trunk().GetName(), tree.RenderOptions{
		Mode:                tree.RenderModeFull,
		HideSummary:         true,
		HideStats:           width < 50,
		SkipSelectionPrefix: true,
		SelectedBranch:      m.engine.CurrentBranchName(),
	})
	lines := make([]string, 1, 1+len(treeLines))
	lines[0] = ds.PaneHeader.Render("stack tree")
	lines = append(lines, treeLines...)
	return lines
}

func (m *companionModel) currentStackRoot() string {
	if m.engine == nil || m.analysis == nil {
		return ""
	}

	currentBranch := m.engine.CurrentBranchName()
	for _, stack := range m.analysis.Stacks {
		if slices.Contains(stack.Stack.AllBranches, currentBranch) {
			return stack.RootBranch()
		}
	}
	return ""
}

func companionStatusIcon(status shippable.Status) string {
	switch status {
	case shippable.StatusShippable:
		return "✓"
	case shippable.StatusPending:
		return "⏳"
	case shippable.StatusBlocked:
		return "✗"
	default:
		return "○"
	}
}

func truncateCompanionLine(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, "…")
}
