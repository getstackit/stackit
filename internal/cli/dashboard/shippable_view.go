package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"stackit.dev/stackit/internal/shippable"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/tui/style"
)

// View renders the dashboard.
func (m *shippableModel) View() string {
	switch m.state {
	case stateHelp:
		return m.renderHelp()
	case stateConfirming:
		return m.renderConfirmation()
	default:
		// All other states (including loading) show the main view
		return m.renderMain()
	}
}

// renderMain renders the main dashboard view.
func (m *shippableModel) renderMain() string {
	// Build sections first, then calculate heights dynamically
	header := m.renderHeader()
	footer := m.renderFooter()

	// Calculate heights using lipgloss.Height() for accurate measurement
	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)

	// Cart panel height (only if items selected)
	// Height: 1 header + N stacks + 1 summary + 2 padding/border
	cartHeight := 0
	if m.cache.selectedCount > 0 {
		cartHeight = 4 + m.cache.selectedCount // Dynamic based on selection count
		if cartHeight > 10 {
			cartHeight = 10 // Cap at reasonable max
		}
	}

	contentHeight := m.Height - headerHeight - footerHeight - cartHeight
	if contentHeight < 5 {
		contentHeight = 5
	}

	leftWidth := m.Width / 2
	rightWidth := m.Width - leftWidth

	// Build panes with calculated dimensions
	leftPane := m.renderStackList(leftWidth, contentHeight)
	rightPane := m.renderDetailsPanel(rightWidth, contentHeight)

	// Combine stack viewer panes (top row)
	stackViewer := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	// Build final layout
	var sections []string
	sections = append(sections, header, stackViewer)

	// Add cart panel if items selected (bottom row)
	if m.cache.selectedCount > 0 {
		cartPane := m.renderCartPanel(m.Width, cartHeight)
		sections = append(sections, cartPane)
	}

	sections = append(sections, footer)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderHeader renders the dashboard header.
func (m *shippableModel) renderHeader() string {
	title := titleStyle.Render("SHIPPABLE WORK")

	var status string
	switch {
	case m.state == stateLoading:
		status = headerStatusStyle.Render(m.Spinner.View() + " Loading...")
	case m.state == stateAnalyzing:
		status = headerStatusStyle.Render(m.Spinner.View() + " Analyzing...")
	case m.state == stateShipping:
		status = headerStatusStyle.Render(m.Spinner.View() + " Shipping...")
	case m.state == statePublishing:
		status = headerStatusStyle.Render(m.Spinner.View() + " Publishing...")
	case m.errorMessage != "":
		status = errorTextStyle.Render(m.errorMessage)
	case m.statusMessage != "":
		status = headerStatusStyle.Render(m.statusMessage)
	case m.analysis != nil:
		status = headerStatusStyle.Render(fmt.Sprintf("%d stacks (%d shippable)",
			m.analysis.TotalStacks(), m.analysis.ShippableCount))
	}

	return headerBorderStyle.
		Width(m.Width).
		Render(lipgloss.JoinHorizontal(lipgloss.Left, title, "  ", status))
}

// renderStackList renders the list of stacks.
func (m *shippableModel) renderStackList(width, height int) string {
	paneStyle := leftPaneStyle.
		Width(width).
		Height(height)

	var sb strings.Builder
	sb.WriteString(paneHeaderStyle.Render("STACKS") + "\n")
	sb.WriteString(strings.Repeat("─", max(width-4, 0)) + "\n")

	// Show appropriate content based on state
	if len(m.stacks) == 0 {
		if m.state == stateLoading {
			sb.WriteString(commonStyles.Dim.Render("Loading stacks..."))
		} else {
			sb.WriteString(commonStyles.Dim.Render("No stacks found. Create one with `stackit create`"))
		}
		// Add help hints at bottom even when empty
		sb.WriteString("\n\n" + style.ColorDim("↑/↓ navigate"))
		return paneStyle.Render(sb.String())
	}

	for i, stack := range m.stacks {
		line := m.renderStackLine(stack, i == m.selectedIndex)
		sb.WriteString(line + "\n")

		// If expanded, show branches
		if m.expanded[stack.RootBranch()] {
			for _, branch := range stack.Stack.AllBranches {
				branchLine := "    " + m.renderBranchLine(branch)
				sb.WriteString(branchLine + "\n")
			}
		}
	}

	// Add contextual shortcuts at bottom of pane
	sb.WriteString("\n" + style.ColorDim("↑/↓ navigate  space select  enter expand  A all"))

	return paneStyle.Render(sb.String())
}

// renderStackLine renders a single stack in the list.
func (m *shippableModel) renderStackLine(stack shippable.Stack, focused bool) string {
	// Cursor indicator for focused row
	cursor := "  "
	if focused {
		cursor = style.ColorCyan("▸ ")
	}

	root := stack.RootBranch()

	// Selection checkbox - show lock icon if locked
	var checkbox string
	switch {
	case m.isLocked(root):
		checkbox = style.ColorYellow("[🔒]")
	case m.selected[root]:
		checkbox = style.ColorCyan("[x]")
	default:
		checkbox = "[ ]"
	}

	// Status icon
	statusIcon := m.getStatusIcon(stack.Status)

	// Stack title: use cached title (computed at refresh time)
	name := m.cache.stackTitles[root]
	if name == "" {
		name = root // Fallback if cache not populated
	}

	// Only show branch count if more than 1 branch
	branchCount := ""
	if count := stack.BranchCount(); count > 1 {
		branchCount = fmt.Sprintf("(%d branches)", count)
	}

	// Expand indicator (only show if stack has multiple branches)
	expandIndicator := ""
	if stack.BranchCount() > 1 {
		expandIndicator = "▶"
		if m.expanded[root] {
			expandIndicator = "▼"
		}
	}

	// Calculate max name length based on available pane width
	// Account for: cursor(2) + checkbox(3) + space(1) + icon(2) + space(1) + branchCount + expandIndicator + padding(4)
	paneWidth := m.Width / 2
	overhead := 2 + 3 + 1 + 2 + 1 + len(branchCount) + len(expandIndicator) + 4
	maxNameLen := paneWidth - overhead
	if maxNameLen < 20 {
		maxNameLen = 20 // Minimum readable length
	}

	// Truncate if needed
	if len(name) > maxNameLen {
		name = name[:maxNameLen-3] + "..."
	}

	// Build line
	var line string
	if branchCount != "" {
		line = fmt.Sprintf("%s%s %s %s %s %s", cursor, checkbox, statusIcon, name, branchCount, expandIndicator)
	} else {
		line = fmt.Sprintf("%s%s %s %s", cursor, checkbox, statusIcon, name)
	}

	// Highlight if focused
	if focused {
		line = selectedRowStyle.Render(line)
	}

	return line
}

// renderBranchLine renders a single branch within an expanded stack.
func (m *shippableModel) renderBranchLine(branchName string) string {
	// Use cached annotation for PR info
	ann, hasCached := m.cache.branchAnnotations[branchName]
	if !hasCached {
		return style.ColorDim("├── " + branchName)
	}

	var prInfo string
	if ann.PRNumber != nil && *ann.PRNumber > 0 {
		prInfo = fmt.Sprintf(" #%d", *ann.PRNumber)
	}

	return style.ColorDim("├── ") + branchName + style.ColorDim(prInfo)
}

// getStatusIcon returns the icon for a stack status.
func (m *shippableModel) getStatusIcon(status shippable.Status) string {
	switch status {
	case shippable.StatusShippable:
		return style.ColorGreen("✓")
	case shippable.StatusPending:
		return style.ColorYellow("⏳")
	case shippable.StatusBlocked:
		return style.ColorRed("✗")
	case shippable.StatusIncomplete:
		return style.ColorDim("○")
	default:
		return "?"
	}
}

// renderDetailsPanel renders the right-side details panel (always shows stack details).
func (m *shippableModel) renderDetailsPanel(width, height int) string {
	paneStyle := rightPaneStyle.
		Width(width).
		Height(height)

	var sb strings.Builder
	sb.WriteString(paneHeaderStyle.Render("DETAILS") + "\n")
	sb.WriteString(strings.Repeat("─", max(width-4, 0)) + "\n")

	if m.focusedStack != nil {
		sb.WriteString(m.renderStackDetails(m.focusedStack))
	} else {
		sb.WriteString(commonStyles.Dim.Render("Select a stack to see details"))
	}

	return paneStyle.Render(sb.String())
}

// renderCartPanel renders the bottom cart panel when stacks are selected.
func (m *shippableModel) renderCartPanel(width, height int) string {
	paneStyle := cartPaneStyle.
		Width(width).
		Height(height)

	// Use cached selected stacks
	selected := m.cache.selectedStacks
	totalBranches := 0
	for _, s := range selected {
		totalBranches += s.BranchCount()
	}

	var sb strings.Builder

	// Header row: title, count badge, and actions
	cartHeader := paneHeaderStyle.Render("SHIPPING")
	countBadge := countBadgeStyle.Render(fmt.Sprintf("%d stacks", len(selected)))

	// Ship button (all selected stacks are shippable by design now)
	shipAction := buttonPrimary.Render("[s] Ship")

	// Combination status
	var analysisStatus string
	if m.combination != nil {
		if m.combination.Combinable {
			analysisStatus = style.ColorGreen("✓ Compatible")
		} else {
			analysisStatus = style.ColorRed("✗ Conflicts")
		}
	} else if len(selected) > 1 {
		analysisStatus = style.ColorDim("[a] analyze compatibility")
	}

	// Header line
	headerLine := fmt.Sprintf("%s %s  %s  %s", cartHeader, countBadge, shipAction, analysisStatus)
	sb.WriteString(headerLine + "\n")

	// Vertical list of selected stacks
	for _, s := range selected {
		statusIcon := m.getStatusIcon(s.Status)
		// Use cached title
		title := m.cache.stackTitles[s.RootBranch()]
		if title == "" {
			title = s.RootBranch()
		}
		// Truncate if needed
		maxLen := width - 10
		if maxLen > 60 {
			maxLen = 60
		}
		if len(title) > maxLen {
			title = title[:maxLen-3] + "..."
		}

		branchInfo := ""
		if s.BranchCount() > 1 {
			branchInfo = style.ColorDim(fmt.Sprintf(" (%d branches)", s.BranchCount()))
		}
		sb.WriteString(fmt.Sprintf("  %s %s%s\n", statusIcon, title, branchInfo))
	}

	// Summary line
	sb.WriteString(style.ColorDim(fmt.Sprintf("  Total: %d branches", totalBranches)))

	return paneStyle.Render(sb.String())
}

// renderStackDetails renders detailed info about a stack with tree view.
func (m *shippableModel) renderStackDetails(stack *shippable.Stack) string {
	var sb strings.Builder

	// Header: show commit title with status badge, branch name dimmed below
	statusBadge := m.renderStatusBadge(stack.Status)
	// Use cached title (computed at refresh time)
	title := m.cache.stackTitles[stack.RootBranch()]
	if title == "" {
		title = stack.RootBranch() // Fallback if cache not populated
	}
	sb.WriteString(commonStyles.Bold.Render(title) + " " + statusBadge + "\n")
	sb.WriteString(style.ColorDim(stack.RootBranch()) + "\n")

	// Quick stats row
	statsRow := m.renderQuickStats(stack)
	sb.WriteString(statsRow + "\n\n")

	// Stack tree visualization
	sb.WriteString(style.ColorDim("Stack:") + "\n")
	treeLines := m.renderStackTree(stack)
	for _, line := range treeLines {
		sb.WriteString(line + "\n")
	}

	// Blocking PRs (if any)
	if len(stack.BlockingPRs) > 0 {
		sb.WriteString("\n" + style.ColorYellow("Blocking:") + "\n")
		for _, bp := range stack.BlockingPRs {
			reason := m.formatBlockingReason(bp.Reason)
			sb.WriteString(fmt.Sprintf("  %s %s\n", style.ColorDim("•"), bp.Branch))
			sb.WriteString(fmt.Sprintf("    %s\n", reason))
		}
	}

	return sb.String()
}

// renderStatusBadge returns a colored badge for the stack status.
func (m *shippableModel) renderStatusBadge(status shippable.Status) string {
	switch status {
	case shippable.StatusShippable:
		return badgeReady.Render("READY")
	case shippable.StatusPending:
		return badgePending.Render("PENDING")
	case shippable.StatusBlocked:
		return badgeBlocked.Render("BLOCKED")
	case shippable.StatusIncomplete:
		return badgeIncomplete.Render("INCOMPLETE")
	default:
		return ""
	}
}

// renderQuickStats shows a compact row of key stats.
func (m *shippableModel) renderQuickStats(stack *shippable.Stack) string {
	var parts []string

	// Branch count
	parts = append(parts, fmt.Sprintf("%d branches", stack.BranchCount()))

	// Approval status
	if stack.ApprovalOK {
		parts = append(parts, style.ColorGreen("✓ Approved"))
	} else {
		parts = append(parts, style.ColorYellow("○ Review needed"))
	}

	// CI status
	if stack.GitHubCIOK {
		parts = append(parts, style.ColorGreen("✓ CI"))
	} else {
		parts = append(parts, style.ColorRed("✗ CI"))
	}

	return style.ColorDim(strings.Join(parts, " • "))
}

// renderStackTree renders the stack as a tree visualization.
func (m *shippableModel) renderStackTree(stack *shippable.Stack) []string {
	// Use cached tree renderer if available, otherwise create one
	var renderer *tree.StackTreeRenderer
	if m.cache.treeRenderer != nil {
		renderer = m.cache.treeRenderer
	} else {
		// Fallback: create a filtered renderer (shouldn't happen after refresh)
		stackBranches := make(map[string]bool)
		for _, branch := range stack.Stack.AllBranches {
			stackBranches[branch] = true
		}
		renderer = tui.NewStackTreeRendererWithFilter(m.engine, func(branchName string) bool {
			return stackBranches[branchName]
		})
	}

	// Build a map of blocking PRs by branch for quick lookup
	blockingByBranch := make(map[string]*shippable.BlockingPR)
	for i := range stack.BlockingPRs {
		bp := &stack.BlockingPRs[i]
		blockingByBranch[bp.Branch] = bp
	}

	// Add annotations for each branch using cached data
	for _, branchName := range stack.Stack.AllBranches {
		// Start with cached annotation (computed at refresh time)
		ann, hasCached := m.cache.branchAnnotations[branchName]
		if !hasCached {
			continue
		}

		// Overlay CI/review status from blocking PRs
		if bp, blocked := blockingByBranch[branchName]; blocked {
			switch bp.Reason {
			case shippable.ReasonCIFailing:
				ann.CheckStatus = tree.CheckStatusFailing
			case shippable.ReasonCIPending:
				ann.CheckStatus = tree.CheckStatusPending
			case shippable.ReasonChangesRequested:
				ann.ReviewStatus = "Changes Requested"
			case shippable.ReasonReviewRequired:
				ann.ReviewStatus = "In Review"
			}
		} else {
			// If not blocked, mark as passing/approved based on stack status
			if stack.GitHubCIOK && ann.CheckStatus == "" {
				ann.CheckStatus = tree.CheckStatusPassing
			}
			if stack.ApprovalOK && ann.ReviewStatus == "" {
				ann.ReviewStatus = "Approved"
			}
		}

		renderer.SetAnnotation(branchName, ann)
	}

	// Render tree in full mode with commit messages to match st info --stack
	opts := tree.RenderOptions{
		Mode:                tree.RenderModeFull,
		HideSummary:         true,
		SkipSelectionPrefix: true,
		ShowCommitMessages:  true,
	}

	return renderer.RenderStack(stack.RootBranch(), opts)
}

// formatBlockingReason returns a human-readable blocking reason.
func (m *shippableModel) formatBlockingReason(reason shippable.BlockingReason) string {
	switch reason {
	case shippable.ReasonCIFailing:
		return style.ColorRed("CI checks failing")
	case shippable.ReasonCIPending:
		return style.ColorYellow("CI checks pending")
	case shippable.ReasonChangesRequested:
		return style.ColorRed("Changes requested")
	case shippable.ReasonReviewRequired:
		return style.ColorYellow("Review required")
	case shippable.ReasonDraft:
		return style.ColorDim("PR is a draft")
	case shippable.ReasonNoPR:
		return style.ColorDim("No PR created")
	default:
		return string(reason)
	}
}

// renderFooter renders the footer with global shortcuts and refresh status.
func (m *shippableModel) renderFooter() string {
	// During async operations, show progress bar
	if m.state == stateLoading || m.state == stateAnalyzing || m.state == stateShipping || m.state == statePublishing {
		return m.renderProgressFooter()
	}

	// Global shortcuts only (pane-specific shortcuts shown in their panes)
	shortcuts := []string{
		"[p] Publish all",
		"[r] Refresh",
		"[?] Help",
		"[q] Quit",
	}
	shortcutsStr := strings.Join(shortcuts, "  ")

	// Build refresh status with countdown
	var refreshStatus string
	if !m.lastRefresh.IsZero() {
		timeSinceRefresh := time.Since(m.lastRefresh)
		timeUntilRefresh := autoRefreshInterval - timeSinceRefresh
		if timeUntilRefresh < 0 {
			timeUntilRefresh = 0
		}
		secondsUntil := int(timeUntilRefresh.Seconds())
		refreshStatus = fmt.Sprintf("Next refresh in %ds", secondsUntil)
	}

	// Build footer: shortcuts on left, refresh status on right
	if refreshStatus != "" {
		gap := m.Width - lipgloss.Width(shortcutsStr) - lipgloss.Width(refreshStatus) - 4
		if gap < 2 {
			gap = 2
		}
		line := strings.Repeat(" ", gap)
		return footerStyle.Width(m.Width).Render(shortcutsStr + line + style.ColorDim(refreshStatus))
	}

	return footerStyle.Width(m.Width).Render(shortcutsStr)
}

// renderProgressFooter renders the footer with a progress bar during async operations.
func (m *shippableModel) renderProgressFooter() string {
	var message string
	switch m.state {
	case stateLoading:
		message = "Refreshing..."
	case stateAnalyzing:
		message = "Analyzing..."
	case stateShipping:
		message = "Shipping..."
	case statePublishing:
		message = "Publishing..."
	}

	if m.progressMessage != "" {
		message = m.progressMessage
	}

	// Build progress line
	var progressLine string
	if m.progressTotal > 0 {
		progressLine = fmt.Sprintf("%s %s (%d/%d)", message, m.progress.View(), m.progressStep, m.progressTotal)
	} else {
		// Show spinner for indeterminate progress
		progressLine = fmt.Sprintf("%s %s", m.Spinner.View(), message)
	}

	quitHint := style.ColorDim("[q] Quit")
	gap := m.Width - lipgloss.Width(progressLine) - lipgloss.Width(quitHint) - 4
	if gap < 2 {
		gap = 2
	}
	line := strings.Repeat(" ", gap)

	return footerStyle.Width(m.Width).Render(progressLine + line + quitHint)
}

// renderHelp renders the help overlay.
func (m *shippableModel) renderHelp() string {
	var sb strings.Builder
	sb.WriteString(helpTitleStyle.Render("Shippable Work Dashboard Help") + "\n\n")

	sb.WriteString(helpSectionStyle.Render("Navigation") + "\n")
	sb.WriteString(helpKeyStyle.Render("j/k, ↑/↓") + helpDescStyle.Render("Move selection up/down") + "\n")
	sb.WriteString(helpKeyStyle.Render("enter") + helpDescStyle.Render("Expand/collapse stack") + "\n")

	sb.WriteString(helpSectionStyle.Render("Selection") + "\n")
	sb.WriteString(helpKeyStyle.Render("space") + helpDescStyle.Render("Toggle stack selection") + "\n")
	sb.WriteString(helpKeyStyle.Render("A") + helpDescStyle.Render("Select all shippable") + "\n")

	sb.WriteString(helpSectionStyle.Render("Actions") + "\n")
	sb.WriteString(helpKeyStyle.Render("s") + helpDescStyle.Render("Ship selected stacks") + "\n")
	sb.WriteString(helpKeyStyle.Render("p") + helpDescStyle.Render("Restack & submit all branches") + "\n")
	sb.WriteString(helpKeyStyle.Render("a") + helpDescStyle.Render("Analyze combination") + "\n")
	sb.WriteString(helpKeyStyle.Render("r") + helpDescStyle.Render("Refresh analysis") + "\n")

	sb.WriteString(helpSectionStyle.Render("Other") + "\n")
	sb.WriteString(helpKeyStyle.Render("?") + helpDescStyle.Render("Toggle this help") + "\n")
	sb.WriteString(helpKeyStyle.Render("q") + helpDescStyle.Render("Quit") + "\n")

	sb.WriteString("\n" + commonStyles.Dim.Render("Press any key to close"))

	return dialogStyle.Render(sb.String())
}

// renderConfirmation renders the ship confirmation dialog.
func (m *shippableModel) renderConfirmation() string {
	// Use cached selected stacks
	selected := m.cache.selectedStacks
	totalBranches := 0
	for _, s := range selected {
		totalBranches += s.BranchCount()
	}

	var sb strings.Builder
	sb.WriteString(helpTitleStyle.Render("SHIP CONFIRMATION") + "\n\n")

	sb.WriteString(fmt.Sprintf("About to ship %d stacks (%d branches total):\n\n",
		len(selected), totalBranches))

	for _, s := range selected {
		sb.WriteString(fmt.Sprintf("  %s (%d branches)\n", s.RootBranch(), s.BranchCount()))
	}

	sb.WriteString("\nThis will:\n")
	sb.WriteString("  - Create consolidation branch\n")
	sb.WriteString("  - Create/update PR to main\n")
	sb.WriteString("  - Merge when CI passes\n")

	sb.WriteString("\n" + strings.Repeat("─", 40) + "\n")
	sb.WriteString("[Enter/y] Confirm  [Esc/n] Cancel\n")

	return dialogStyle.Render(sb.String())
}
