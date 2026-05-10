package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/tui/style"
)

// storyStack represents a simulated stack for the storyboard.
// This mirrors shippable.Stack but avoids the import cycle.
type storyStack struct {
	rootBranch  string
	allBranches []string
	status      storyStackStatus
	author      string
	prTitle     string
	stackTitle  string
	stackDesc   string
	approvalOK  bool
	gitHubCIOK  bool
	blockingPRs []storyBlockingPR
}

// storyStackStatus represents the shippability status.
type storyStackStatus string

const (
	storyStatusShippable  storyStackStatus = "shippable"
	storyStatusPending    storyStackStatus = "pending"
	storyStatusBlocked    storyStackStatus = "blocked"
	storyStatusIncomplete storyStackStatus = "incomplete"
)

// storyBlockingPR describes a blocking PR.
type storyBlockingPR struct {
	branch   string
	prNumber int
	reason   storyBlockingReason
}

// storyBlockingReason describes why a PR is blocking.
type storyBlockingReason string

const (
	storyReasonChangesRequested storyBlockingReason = "changes_requested"
	storyReasonCIFailing        storyBlockingReason = "ci_failing"
	storyReasonCIPending        storyBlockingReason = "ci_pending"
	storyReasonDraft            storyBlockingReason = "draft"
	storyReasonNoPR             storyBlockingReason = "no_pr"
	storyReasonReviewRequired   storyBlockingReason = "review_required"
)

// branchCount returns the number of branches in the stack.
func (s *storyStack) branchCount() int {
	return len(s.allBranches)
}

// shippableScenario defines a simulated dashboard state for the storyboard.
type shippableScenario struct {
	name        string
	description string
	stacks      []storyStack
	// Optional: pre-selected stacks by root branch
	selectedStacks []string
	// Optional: simulate a dashboard state
	state dashboardStoryState
}

// dashboardStoryState represents the simulated dashboard state.
type dashboardStoryState int

const (
	stateStoryMain dashboardStoryState = iota
	stateStoryLoading
	stateStoryAnalyzing
	stateStoryShipping
)

// Step status constants for merge simulation
const (
	stepStatusPending = "pending"
	stepStatusRunning = "running"
	stepStatusWaiting = "waiting"
	stepStatusDone    = "done"
	stepStatusError   = "error"
)

// Predefined scenarios for testing different dashboard states
var shippableScenarios = []shippableScenario{
	{
		name:        "Mixed Stack States",
		description: "Dashboard showing shippable, pending, blocked, and incomplete stacks",
		stacks:      createMixedStacksScenario(),
		state:       stateStoryMain,
	},
	{
		name:        "All Shippable",
		description: "Multiple stacks ready to ship",
		stacks:      createAllShippableScenario(),
		state:       stateStoryMain,
	},
	{
		name:        "With Selection",
		description: "Dashboard with stacks selected for shipping",
		stacks:      createMixedStacksScenario(),
		selectedStacks: []string{
			"jonnii/20250101/add-auth-flow",
			"jonnii/20250102/api-refactor",
		},
		state: stateStoryMain,
	},
	{
		name:        "Analyzing Combination",
		description: "Dashboard analyzing if selected stacks can ship together",
		stacks:      createAllShippableScenario(),
		selectedStacks: []string{
			"jonnii/20250101/add-auth-flow",
			"jonnii/20250102/api-refactor",
		},
		state: stateStoryAnalyzing,
	},
	{
		name:        "Shipping In Progress",
		description: "Dashboard with shipping operation in progress",
		stacks:      createAllShippableScenario(),
		selectedStacks: []string{
			"jonnii/20250101/add-auth-flow",
		},
		state: stateStoryShipping,
	},
	{
		name:        "Large Stack",
		description: "A deeply nested stack with many branches",
		stacks:      createLargeStackScenario(),
		state:       stateStoryMain,
	},
	{
		name:        "Stack Descriptions",
		description: "Stacks with varied description content in the details panel",
		stacks:      createStackDescriptionsScenario(),
		state:       stateStoryMain,
	},
}

// createMixedStacksScenario creates a scenario with different stack states.
func createMixedStacksScenario() []storyStack {
	return []storyStack{
		{
			rootBranch:  "jonnii/20250101/add-auth-flow",
			allBranches: []string{"jonnii/20250101/add-auth-flow", "jonnii/20250101/add-auth-validation", "jonnii/20250101/add-auth-login"},
			status:      storyStatusShippable,
			author:      "jonnii",
			prTitle:     "feat: Add authentication flow",
			stackTitle:  "Authentication System",
			stackDesc:   "Adds JWT-based authentication with **login**, **validation**, and **token refresh** endpoints.",
			approvalOK:  true,
			gitHubCIOK:  true,
		},
		{
			rootBranch:  "jonnii/20250102/api-refactor",
			allBranches: []string{"jonnii/20250102/api-refactor"},
			status:      storyStatusShippable,
			author:      "jonnii",
			prTitle:     "refactor: Clean up API layer",
			approvalOK:  true,
			gitHubCIOK:  true,
		},
		{
			rootBranch:  "jonnii/20250103/fix-ci-pipeline",
			allBranches: []string{"jonnii/20250103/fix-ci-pipeline", "jonnii/20250103/fix-ci-caching"},
			status:      storyStatusPending,
			author:      "jonnii",
			prTitle:     "fix: CI pipeline caching",
			approvalOK:  true,
			gitHubCIOK:  false,
			blockingPRs: []storyBlockingPR{
				{branch: "jonnii/20250103/fix-ci-caching", prNumber: 205, reason: storyReasonCIPending},
			},
		},
		{
			rootBranch:  "jonnii/20250104/database-migration",
			allBranches: []string{"jonnii/20250104/database-migration"},
			status:      storyStatusBlocked,
			author:      "jonnii",
			prTitle:     "feat: Database schema migration",
			approvalOK:  false,
			gitHubCIOK:  false,
			blockingPRs: []storyBlockingPR{
				{branch: "jonnii/20250104/database-migration", prNumber: 210, reason: storyReasonChangesRequested},
			},
		},
		{
			rootBranch:  "jonnii/20250105/wip-feature",
			allBranches: []string{"jonnii/20250105/wip-feature"},
			status:      storyStatusIncomplete,
			author:      "jonnii",
			prTitle:     "WIP: New feature exploration",
			approvalOK:  false,
			gitHubCIOK:  false,
			blockingPRs: []storyBlockingPR{
				{branch: "jonnii/20250105/wip-feature", reason: storyReasonDraft},
			},
		},
	}
}

// createAllShippableScenario creates a scenario where all stacks are shippable.
func createAllShippableScenario() []storyStack {
	return []storyStack{
		{
			rootBranch:  "jonnii/20250101/add-auth-flow",
			allBranches: []string{"jonnii/20250101/add-auth-flow", "jonnii/20250101/add-auth-validation", "jonnii/20250101/add-auth-login"},
			status:      storyStatusShippable,
			author:      "jonnii",
			prTitle:     "feat: Add authentication flow",
			approvalOK:  true,
			gitHubCIOK:  true,
		},
		{
			rootBranch:  "jonnii/20250102/api-refactor",
			allBranches: []string{"jonnii/20250102/api-refactor", "jonnii/20250102/api-cleanup"},
			status:      storyStatusShippable,
			author:      "jonnii",
			prTitle:     "refactor: Clean up API layer",
			approvalOK:  true,
			gitHubCIOK:  true,
		},
		{
			rootBranch:  "jonnii/20250103/fix-tests",
			allBranches: []string{"jonnii/20250103/fix-tests"},
			status:      storyStatusShippable,
			author:      "jonnii",
			prTitle:     "fix: Flaky test suite",
			approvalOK:  true,
			gitHubCIOK:  true,
		},
	}
}

// createLargeStackScenario creates a deeply nested stack.
func createLargeStackScenario() []storyStack {
	return []storyStack{
		{
			rootBranch: "jonnii/20250110/large-feature",
			allBranches: []string{
				"jonnii/20250110/large-feature",
				"jonnii/20250110/large-feature-part2",
				"jonnii/20250110/large-feature-part3",
				"jonnii/20250110/large-feature-part4",
				"jonnii/20250110/large-feature-part5",
				"jonnii/20250110/large-feature-tests",
				"jonnii/20250110/large-feature-docs",
			},
			status:     storyStatusPending,
			author:     "jonnii",
			prTitle:    "feat: Large multi-part feature implementation",
			approvalOK: true,
			gitHubCIOK: false,
			blockingPRs: []storyBlockingPR{
				{branch: "jonnii/20250110/large-feature-part5", prNumber: 220, reason: storyReasonCIPending},
			},
		},
	}
}

// createStackDescriptionsScenario creates stacks with varied description content.
func createStackDescriptionsScenario() []storyStack {
	return []storyStack{
		{
			rootBranch:  "jonnii/20250201/config-system",
			allBranches: []string{"jonnii/20250201/config-system", "jonnii/20250201/config-validation"},
			status:      storyStatusShippable,
			author:      "jonnii",
			prTitle:     "feat: Layered configuration system",
			stackTitle:  "Configuration Overhaul",
			stackDesc:   "Replaces the flat config with a **layered system** supporting:\n- Global defaults\n- Project-level overrides\n- Environment variables",
			approvalOK:  true,
			gitHubCIOK:  true,
		},
		{
			rootBranch:  "jonnii/20250202/perf-improvements",
			allBranches: []string{"jonnii/20250202/perf-improvements"},
			status:      storyStatusShippable,
			author:      "jonnii",
			prTitle:     "perf: Reduce git operations",
			stackTitle:  "Performance Pass",
			approvalOK:  true,
			gitHubCIOK:  true,
		},
		{
			rootBranch:  "jonnii/20250203/error-handling",
			allBranches: []string{"jonnii/20250203/error-handling", "jonnii/20250203/error-display"},
			status:      storyStatusPending,
			author:      "jonnii",
			prTitle:     "fix: Better error messages",
			stackTitle:  "Error Handling Improvements",
			stackDesc:   "Wraps all git errors with **actionable context** so users know how to recover.",
			approvalOK:  true,
			gitHubCIOK:  false,
			blockingPRs: []storyBlockingPR{
				{branch: "jonnii/20250203/error-display", prNumber: 250, reason: storyReasonCIPending},
			},
		},
	}
}

// shippableStoryModel is a simplified dashboard model for the storyboard.
// It doesn't require real Engine or GitHub client.
type shippableStoryModel struct {
	scenario shippableScenario

	// UI state
	selectedIndex int
	expanded      map[string]bool
	selected      map[string]bool
	width         int
	height        int

	// Spinner for loading states
	spinner spinner.Model

	// Styles - uses shared dashboard styles for consistency with real app
	ds style.DashboardStyles
}

// storyBadges maps story status to the shared dashboard badge styles.
var storyBadges = func() map[storyStackStatus]lipgloss.Style {
	ds := style.DefaultDashboardStyles()
	return map[storyStackStatus]lipgloss.Style{
		storyStatusShippable:  ds.BadgeReady,
		storyStatusPending:    ds.BadgePending,
		storyStatusBlocked:    ds.BadgeBlocked,
		storyStatusIncomplete: ds.BadgeIncomplete,
	}
}()

func newShippableStoryModel(scenario shippableScenario) *shippableStoryModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	m := &shippableStoryModel{
		scenario: scenario,
		expanded: make(map[string]bool),
		selected: make(map[string]bool),
		spinner:  s,
		ds:       style.DefaultDashboardStyles(),
	}

	// Apply pre-selections
	for _, root := range scenario.selectedStacks {
		m.selected[root] = true
	}

	return m
}

func (m *shippableStoryModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m *shippableStoryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case core.KeyQuit, core.KeyEsc:
			return m, tea.Quit
		case core.KeyUp, "k":
			if m.selectedIndex > 0 {
				m.selectedIndex--
			}
		case core.KeyDown, "j":
			if m.selectedIndex < len(m.scenario.stacks)-1 {
				m.selectedIndex++
			}
		case "space", " ":
			m.toggleSelection()
		case core.KeyEnter:
			m.toggleExpand()
		case "A":
			m.selectAllShippable()
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *shippableStoryModel) toggleSelection() {
	if m.selectedIndex >= len(m.scenario.stacks) {
		return
	}
	stack := m.scenario.stacks[m.selectedIndex]
	if stack.status != storyStatusShippable {
		return
	}
	root := stack.rootBranch
	m.selected[root] = !m.selected[root]
}

func (m *shippableStoryModel) toggleExpand() {
	if m.selectedIndex >= len(m.scenario.stacks) {
		return
	}
	stack := m.scenario.stacks[m.selectedIndex]
	root := stack.rootBranch
	m.expanded[root] = !m.expanded[root]
}

func (m *shippableStoryModel) selectAllShippable() {
	for _, s := range m.scenario.stacks {
		if s.status == storyStatusShippable {
			m.selected[s.rootBranch] = true
		}
	}
}

func (m *shippableStoryModel) View() tea.View {
	if m.width == 0 {
		return tea.NewView("Loading...")
	}

	header := m.renderHeader()
	footer := m.renderFooter()

	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)

	// Action bar height (fixed 3 lines when visible)
	selectedCount := m.countSelected()
	actionBarHeight := 0
	if selectedCount > 0 {
		actionBarHeight = 3
	}

	contentHeight := m.height - headerHeight - footerHeight - actionBarHeight
	contentHeight = max(contentHeight, 5)

	leftWidth := m.width / 2
	rightWidth := m.width - leftWidth

	leftPane := m.renderStackList(leftWidth, contentHeight)
	rightPane := m.renderDetailsPanel(rightWidth, contentHeight)

	stackViewer := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	var sections []string
	sections = append(sections, header, stackViewer)

	if selectedCount > 0 {
		bar := m.renderActionBar(m.width)
		sections = append(sections, bar)
	}

	sections = append(sections, footer)

	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

func (m *shippableStoryModel) renderHeader() string {
	title := m.ds.Title.Render("SHIPPABLE WORK")

	var status string
	switch m.scenario.state {
	case stateStoryLoading:
		status = m.spinner.View() + " Loading..."
	case stateStoryAnalyzing:
		status = m.spinner.View() + " Analyzing combination..."
	case stateStoryShipping:
		status = m.spinner.View() + " Shipping..."
	default:
		shippableCount := 0
		for _, s := range m.scenario.stacks {
			if s.status == storyStatusShippable {
				shippableCount++
			}
		}
		status = fmt.Sprintf("%d stacks (%d shippable)", len(m.scenario.stacks), shippableCount)
	}

	return m.ds.HeaderBorder.
		Width(m.width).
		Render(lipgloss.JoinHorizontal(lipgloss.Left, title, "  ", status))
}

func (m *shippableStoryModel) renderStackList(width, height int) string {
	borderW := m.ds.LeftPane.GetHorizontalBorderSize()
	borderH := m.ds.LeftPane.GetVerticalBorderSize()
	paneStyle := m.ds.LeftPane.
		Width(width - borderW).
		Height(height - borderH)

	var sb strings.Builder
	sb.WriteString(m.ds.PaneHeader.Render("STACKS") + "\n\n")

	for i, stack := range m.scenario.stacks {
		line := m.renderStackLine(stack, i == m.selectedIndex)
		sb.WriteString(line + "\n")

		if m.expanded[stack.rootBranch] {
			for _, branch := range stack.allBranches {
				sb.WriteString("    " + style.ColorDim("├── "+branch) + "\n")
			}
		}
	}

	sb.WriteString("\n" + style.ColorDim("↑/↓ navigate  space select  enter expand  A all"))

	return paneStyle.Render(sb.String())
}

func (m *shippableStoryModel) renderStackLine(stack storyStack, focused bool) string {
	cursor := "  "
	if focused {
		cursor = style.ColorCyan("▸ ")
	}

	root := stack.rootBranch

	// Pad checkbox and status icon to fixed widths for alignment
	var checkbox string
	if m.selected[root] {
		checkbox = style.ColorCyan("[x]")
	} else {
		checkbox = "[ ]"
	}
	checkbox = style.PadToWidth(checkbox, style.CheckboxColumnWidth)

	statusIcon := style.PadToWidth(m.getStatusIcon(stack.status), style.StatusIconColumnWidth)

	name := stack.prTitle
	if name == "" {
		name = root
	}

	branchCount := ""
	if count := stack.branchCount(); count > 1 {
		branchCount = fmt.Sprintf("(%d branches)", count)
	}

	expandIndicator := ""
	if stack.branchCount() > 1 {
		expandIndicator = "▶"
		if m.expanded[root] {
			expandIndicator = "▼"
		}
	}

	var line string
	if branchCount != "" {
		line = fmt.Sprintf("%s%s %s %s %s %s", cursor, checkbox, statusIcon, name, branchCount, expandIndicator)
	} else {
		line = fmt.Sprintf("%s%s %s %s", cursor, checkbox, statusIcon, name)
	}

	if focused {
		line = m.ds.SelectedRow.Render(line)
	}

	return line
}

func (m *shippableStoryModel) getStatusIcon(status storyStackStatus) string {
	switch status {
	case storyStatusShippable:
		return style.ColorGreen("✓")
	case storyStatusPending:
		return style.ColorYellow("⏳")
	case storyStatusBlocked:
		return style.ColorRed("✗")
	case storyStatusIncomplete:
		return style.ColorDim("○")
	default:
		return "?"
	}
}

func (m *shippableStoryModel) renderDetailsPanel(width, height int) string {
	borderW := m.ds.RightPane.GetHorizontalBorderSize()
	borderH := m.ds.RightPane.GetVerticalBorderSize()
	paneStyle := m.ds.RightPane.
		Width(width - borderW).
		Height(height - borderH)

	var sb strings.Builder
	sb.WriteString(m.ds.PaneHeader.Render("DETAILS") + "\n\n")

	if m.selectedIndex < len(m.scenario.stacks) {
		stack := &m.scenario.stacks[m.selectedIndex]
		sb.WriteString(m.renderStackDetails(stack))
	} else {
		sb.WriteString(style.ColorDim("Select a stack to see details"))
	}

	return paneStyle.Render(sb.String())
}

func (m *shippableStoryModel) renderStackDetails(stack *storyStack) string {
	var sb strings.Builder

	// Show stack description if present (matches real dashboard's logic)
	if stack.stackTitle != "" {
		var markdown string
		if stack.stackDesc != "" {
			markdown = "# " + stack.stackTitle + "\n\n" + stack.stackDesc
		} else {
			markdown = "# " + stack.stackTitle
		}
		rendered := style.RenderMarkdown(markdown)
		sb.WriteString(rendered + "\n")
	}

	// Header
	title := stack.prTitle
	if title == "" {
		title = stack.rootBranch
	}
	badge := storyBadges[stack.status].Render(strings.ToUpper(string(stack.status)))
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title) + " " + badge + "\n")
	sb.WriteString(style.ColorDim(stack.rootBranch) + "\n\n")

	// Quick stats
	var parts []string
	parts = append(parts, fmt.Sprintf("%d branches", stack.branchCount()))
	if stack.approvalOK {
		parts = append(parts, style.ColorGreen("✓ Approved"))
	} else {
		parts = append(parts, style.ColorYellow("○ Review needed"))
	}
	if stack.gitHubCIOK {
		parts = append(parts, style.ColorGreen("✓ CI"))
	} else {
		parts = append(parts, style.ColorRed("✗ CI"))
	}
	sb.WriteString(style.ColorDim(strings.Join(parts, " • ")) + "\n\n")

	// Stack tree (simplified)
	sb.WriteString(style.ColorDim("Stack:") + "\n")
	for i, branch := range stack.allBranches {
		prefix := "├──"
		if i == len(stack.allBranches)-1 {
			prefix = "└──"
		}
		fmt.Fprintf(&sb, "  %s %s\n", style.ColorDim(prefix), branch)
	}

	// Blocking PRs
	if len(stack.blockingPRs) > 0 {
		sb.WriteString("\n" + style.ColorYellow("Blocking:") + "\n")
		for _, bp := range stack.blockingPRs {
			reason := m.formatBlockingReason(bp.reason)
			fmt.Fprintf(&sb, "  %s %s\n", style.ColorDim("•"), bp.branch)
			fmt.Fprintf(&sb, "    %s\n", reason)
		}
	}

	return sb.String()
}

func (m *shippableStoryModel) formatBlockingReason(reason storyBlockingReason) string {
	switch reason {
	case storyReasonCIFailing:
		return style.ColorRed("CI checks failing")
	case storyReasonCIPending:
		return style.ColorYellow("CI checks pending")
	case storyReasonChangesRequested:
		return style.ColorRed("Changes requested")
	case storyReasonReviewRequired:
		return style.ColorYellow("Review required")
	case storyReasonDraft:
		return style.ColorDim("PR is a draft")
	case storyReasonNoPR:
		return style.ColorDim("No PR created")
	default:
		return string(reason)
	}
}

func (m *shippableStoryModel) countSelected() int {
	count := 0
	for _, s := range m.scenario.stacks {
		if m.selected[s.rootBranch] {
			count++
		}
	}
	return count
}

func (m *shippableStoryModel) renderActionBar(width int) string {
	barStyle := m.ds.ActionBar.Width(width - m.ds.ActionBar.GetHorizontalBorderSize())

	totalBranches := 0
	selectedCount := 0
	for _, s := range m.scenario.stacks {
		if m.selected[s.rootBranch] {
			selectedCount++
			totalBranches += s.branchCount()
		}
	}

	summary := fmt.Sprintf("%d stacks selected (%d branches)", selectedCount, totalBranches)
	shipAction := m.ds.ButtonPrimary.Render("[s] Ship")

	var analysisAction string
	if selectedCount > 1 {
		analysisAction = style.ColorDim("[a] Analyze")
	}

	line := fmt.Sprintf("%s  %s  %s", summary, shipAction, analysisAction)
	return barStyle.Render(line)
}

func (m *shippableStoryModel) renderFooter() string {
	if m.scenario.state == stateStoryLoading || m.scenario.state == stateStoryAnalyzing || m.scenario.state == stateStoryShipping {
		var message string
		switch m.scenario.state {
		case stateStoryLoading:
			message = "Refreshing..."
		case stateStoryAnalyzing:
			message = "Analyzing combination..."
		case stateStoryShipping:
			message = "Shipping stacks..."
		}
		return m.ds.Footer.Width(m.width).Render(m.spinner.View() + " " + message)
	}

	shortcuts := "[p] Publish all  [r] Refresh  [?] Help  [q] Quit"
	return m.ds.Footer.Width(m.width).Render(shortcuts)
}

// registerShippableStories registers all shippable dashboard stories.
func registerShippableStories() {
	for _, scenario := range shippableScenarios {
		s := scenario // Capture for closure
		RegisterStory(Story{
			Name:        s.name,
			Category:    "Shippable",
			Description: s.description,
			CreateModel: func() tea.Model {
				return newShippableStoryModel(s)
			},
		})
	}

	// Also register merge progress flow stories
	registerMergeProgressStories()
}

// registerMergeProgressStories adds stories for merge progress simulation.
func registerMergeProgressStories() {
	RegisterStory(Story{
		Name:        "Merge Progress - Happy Path",
		Category:    "Merge Flow",
		Description: "Simulated merge progress with all steps succeeding",
		CreateModel: func() tea.Model {
			return newMergeProgressSimulation(mergeScenarioHappyPath)
		},
	})

	RegisterStory(Story{
		Name:        "Merge Progress - Waiting for CI",
		Category:    "Merge Flow",
		Description: "Simulated merge progress stuck waiting for CI checks",
		CreateModel: func() tea.Model {
			return newMergeProgressSimulation(mergeScenarioWaitingCI)
		},
	})

	RegisterStory(Story{
		Name:        "Merge Progress - Failure",
		Category:    "Merge Flow",
		Description: "Simulated merge progress with a failure",
		CreateModel: func() tea.Model {
			return newMergeProgressSimulation(mergeScenarioFailure)
		},
	})
}

// mergeScenarioType defines different merge progress scenarios
type mergeScenarioType int

const (
	mergeScenarioHappyPath mergeScenarioType = iota
	mergeScenarioWaitingCI
	mergeScenarioFailure
)

// mergeProgressSimulation simulates the merge progress component
type mergeProgressSimulation struct {
	scenario  mergeScenarioType
	step      int
	startTime time.Time
	spinner   spinner.Model

	// Simulated state
	groups []mergeGroup
	steps  []mergeStep
}

type mergeGroup struct {
	label       string
	stepIndices []int
}

type mergeStep struct {
	description string
	status      string // pending, running, waiting, done, error
	elapsed     time.Duration
}

func newMergeProgressSimulation(scenario mergeScenarioType) *mergeProgressSimulation {
	s := spinner.New()
	s.Spinner = spinner.Dot

	// Create mock merge plan based on scenario
	groups := []mergeGroup{
		{label: "Merge feature/auth-base (#101)", stepIndices: []int{0}},
		{label: "Merge feature/auth-validation (#102)", stepIndices: []int{1}},
		{label: "Merge feature/auth-login (#103)", stepIndices: []int{2}},
		{label: "Cleanup local branches", stepIndices: []int{3}},
	}

	steps := []mergeStep{
		{description: "Merge PR #101", status: stepStatusPending},
		{description: "Merge PR #102", status: stepStatusPending},
		{description: "Merge PR #103", status: stepStatusPending},
		{description: "Delete local branches", status: stepStatusPending},
	}

	return &mergeProgressSimulation{
		scenario:  scenario,
		startTime: time.Now(),
		spinner:   s,
		groups:    groups,
		steps:     steps,
	}
}

func (m *mergeProgressSimulation) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.nextTick(),
	)
}

func (m *mergeProgressSimulation) nextTick() tea.Cmd {
	delay := 1500 * time.Millisecond
	if m.step == 0 {
		delay = 500 * time.Millisecond
	}
	return tea.Tick(delay, func(_ time.Time) tea.Msg {
		return mergeSimTickMsg(m.step)
	})
}

type mergeSimTickMsg int

func (m *mergeProgressSimulation) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == core.KeyQuit || msg.String() == core.KeyEsc {
			return m, tea.Quit
		}
		if msg.String() == "r" {
			// Reset simulation
			m.step = 0
			for i := range m.steps {
				m.steps[i].status = stepStatusPending
				m.steps[i].elapsed = 0
			}
			return m, m.nextTick()
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case mergeSimTickMsg:
		return m.advanceSimulation()
	}

	return m, nil
}

func (m *mergeProgressSimulation) advanceSimulation() (tea.Model, tea.Cmd) {
	m.step++

	switch m.scenario {
	case mergeScenarioHappyPath:
		return m.advanceHappyPath()
	case mergeScenarioWaitingCI:
		return m.advanceWaitingCI()
	case mergeScenarioFailure:
		return m.advanceFailure()
	}

	return m, nil
}

func (m *mergeProgressSimulation) advanceHappyPath() (tea.Model, tea.Cmd) {
	switch m.step {
	case 1:
		m.steps[0].status = stepStatusRunning
	case 2:
		m.steps[0].status = stepStatusDone
		m.steps[1].status = stepStatusRunning
	case 3:
		m.steps[1].status = stepStatusDone
		m.steps[2].status = stepStatusRunning
	case 4:
		m.steps[2].status = stepStatusDone
		m.steps[3].status = stepStatusRunning
	case 5:
		m.steps[3].status = stepStatusDone
		// All done, reset after a pause
		return m, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
			return mergeSimTickMsg(-1)
		})
	case -1:
		// Reset
		m.step = 0
		for i := range m.steps {
			m.steps[i].status = stepStatusPending
		}
	}
	return m, m.nextTick()
}

func (m *mergeProgressSimulation) advanceWaitingCI() (tea.Model, tea.Cmd) {
	switch {
	case m.step == 1:
		m.steps[0].status = stepStatusRunning
	case m.step == 2:
		m.steps[0].status = stepStatusWaiting
		m.steps[0].elapsed = 30 * time.Second
	case m.step >= 3 && m.step <= 6:
		// Continue waiting with increasing elapsed time
		m.steps[0].elapsed = time.Duration(30*(m.step-1)) * time.Second
	case m.step == 7:
		m.steps[0].status = stepStatusDone
		m.steps[1].status = stepStatusRunning
	case m.step == 8:
		m.steps[1].status = stepStatusDone
		m.steps[2].status = stepStatusRunning
	case m.step == 9:
		m.steps[2].status = stepStatusDone
		m.steps[3].status = stepStatusRunning
	case m.step == 10:
		m.steps[3].status = stepStatusDone
		return m, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
			return mergeSimTickMsg(-1)
		})
	case m.step == -1:
		m.step = 0
		for i := range m.steps {
			m.steps[i].status = stepStatusPending
			m.steps[i].elapsed = 0
		}
	}
	return m, m.nextTick()
}

func (m *mergeProgressSimulation) advanceFailure() (tea.Model, tea.Cmd) {
	switch m.step {
	case 1:
		m.steps[0].status = stepStatusRunning
	case 2:
		m.steps[0].status = stepStatusDone
		m.steps[1].status = stepStatusRunning
	case 3:
		m.steps[1].status = stepStatusError
		// Stay in error state, then reset
		return m, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
			return mergeSimTickMsg(-1)
		})
	case -1:
		m.step = 0
		for i := range m.steps {
			m.steps[i].status = stepStatusPending
		}
	}
	return m, m.nextTick()
}

func (m *mergeProgressSimulation) View() tea.View {
	var b strings.Builder
	b.WriteString("\n")

	// Header
	completedCount := 0
	for _, s := range m.steps {
		if s.status == stepStatusDone {
			completedCount++
		}
	}

	header := lipgloss.NewStyle().Bold(true).Render("Merge Progress")
	progress := style.ColorDim(fmt.Sprintf("  Step %d of %d", completedCount+1, len(m.groups)))
	b.WriteString(header + progress + "\n\n")

	// Render each group
	for i, group := range m.groups {
		step := m.steps[group.stepIndices[0]]
		var icon string
		var labelStyle lipgloss.Style

		switch step.status {
		case stepStatusDone:
			icon = style.ColorGreen("✓")
			labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
		case stepStatusRunning:
			icon = m.spinner.View()
			labelStyle = lipgloss.NewStyle().Bold(true)
		case stepStatusWaiting:
			icon = m.spinner.View()
			labelStyle = lipgloss.NewStyle().Bold(true)
		case stepStatusError:
			icon = style.ColorRed("✗")
			labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		default:
			icon = style.ColorDim("○")
			labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		}

		line := fmt.Sprintf("  %s %s", icon, labelStyle.Render(group.label))

		// Add waiting info
		if step.status == stepStatusWaiting {
			line += style.ColorYellow(fmt.Sprintf(" [waiting for CI: %v]", step.elapsed))
		}

		// Add error info
		if step.status == stepStatusError {
			line += style.ColorRed(" → merge conflict detected")
		}

		b.WriteString(line + "\n")

		// Show CI details when waiting
		if step.status == stepStatusWaiting {
			b.WriteString("    └ " + style.ColorDim("lint (running), test (running), build (queued)") + "\n")
		}

		// Only show a few pending steps
		if step.status == stepStatusPending && i > completedCount+2 {
			remaining := len(m.groups) - i
			fmt.Fprintf(&b, style.ColorDim("  ... %d more steps\n"), remaining)
			break
		}
	}

	// Summary
	if allStepsDone(m.steps) {
		b.WriteString("\n" + style.ColorGreen("✓ All steps completed successfully") + "\n")
	}

	b.WriteString("\n" + style.ColorDim("[r] restart simulation  [q] back"))

	return tea.NewView(b.String())
}

func allStepsDone(steps []mergeStep) bool {
	for _, s := range steps {
		if s.status != stepStatusDone {
			return false
		}
	}
	return true
}

func init() {
	registerShippableStories()
}
