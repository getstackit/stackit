package dashboard

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/shippable"
)

// Update handles all messages and updates the model.
func (m *shippableModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle window size messages
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width = wsMsg.Width
		m.Height = wsMsg.Height
		// Also update progress bar width
		m.progress.SetWidth(min(wsMsg.Width-20, 60))
		return m, nil
	}

	// Handle spinner ticks for loading animations
	if handled, cmd := m.HandleSpinnerMsg(msg); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKeyMsg(msg)

	case tickMsg:
		// Check if auto-refresh is needed (only in main state, not during other operations)
		if m.state == stateMain && !m.lastRefresh.IsZero() {
			timeSinceRefresh := time.Since(m.lastRefresh)
			if timeSinceRefresh >= autoRefreshInterval {
				m.state = stateLoading
				m.progressMessage = "Auto-refreshing..."
				return m, tea.Batch(m.refresh(), m.tick())
			}
		}
		// Continue ticking for countdown display
		return m, m.tick()

	case progress.FrameMsg:
		// Update progress bar animation
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		return m, cmd

	case progressUpdateMsg:
		// Update progress state
		m.progressStep = msg.step
		m.progressTotal = msg.total
		m.progressMessage = msg.message
		// Animate the progress bar
		if msg.total > 0 {
			percent := float64(msg.step) / float64(msg.total)
			return m, m.progress.SetPercent(percent)
		}
		return m, nil

	case refreshCompleteMsg:
		m.state = stateMain
		m.lastRefresh = time.Now()
		m.progressStep = 0
		m.progressTotal = 0
		m.progressMessage = ""
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return m, nil
		}
		m.errorMessage = ""
		m.analysis = msg.result
		m.stacks = msg.result.Stacks
		m.rebuildCache() // Precompute titles, annotations, and tree renderer

		// On initial load, auto-focus the stack containing the checked-out branch
		if m.initialLoad && m.cache.currentStackRoot != "" {
			for i, stack := range m.stacks {
				if stack.RootBranch() == m.cache.currentStackRoot {
					m.selectedIndex = i
					break
				}
			}
			m.initialLoad = false
		}

		m.updateFocusedStack()
		return m, nil

	case analysisCompleteMsg:
		m.state = stateMain
		m.progressStep = 0
		m.progressTotal = 0
		m.progressMessage = ""
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return m, nil
		}
		m.analysis = msg.result
		m.stacks = msg.result.Stacks
		m.rebuildCache() // Precompute titles, annotations, and tree renderer
		m.statusMessage = "Analysis complete"
		return m, nil

	case combinationCompleteMsg:
		m.state = stateMain
		m.progressStep = 0
		m.progressTotal = 0
		m.progressMessage = ""
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return m, nil
		}
		m.combination = msg.result
		if msg.result.Combinable {
			m.statusMessage = "Selected stacks can be combined"
		} else {
			m.statusMessage = "Some stacks conflict"
		}
		return m, nil

	case shipCompleteMsg:
		m.state = stateMain
		m.progressStep = 0
		m.progressTotal = 0
		m.progressMessage = ""
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return m, nil
		}
		// Ship successful
		m.statusMessage = "Shipped! PR: " + msg.result.PRURL
		m.clearSelection()
		return m, m.refresh()

	case submitCompleteMsg:
		m.state = stateMain
		m.progressStep = 0
		m.progressTotal = 0
		m.progressMessage = ""
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return m, nil
		}
		m.statusMessage = fmt.Sprintf("Submitted %d branches", msg.submitted)
		return m, m.refresh()

	case squashCompleteMsg:
		m.state = stateMain
		m.progressStep = 0
		m.progressTotal = 0
		m.progressMessage = ""
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return m, nil
		}
		m.statusMessage = fmt.Sprintf("Squashed %s", msg.branch)
		return m, m.refresh()
	}

	return m, nil
}

// handleKeyMsg handles keyboard input.
func (m *shippableModel) handleKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// If showing confirmation, handle confirm/cancel
	if m.state == stateConfirming {
		return m.handleConfirmationKey(msg)
	}

	// If showing help, any key closes it
	if m.state == stateHelp {
		m.state = stateMain
		return m, nil
	}

	// If loading, analyzing, shipping, squashing, or publishing, only allow quit
	if m.state == stateLoading || m.state == stateAnalyzing || m.state == stateShipping || m.state == stateSubmitting || m.state == stateSquashing {
		if key.Matches(msg, keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

	// Handle pane switching (available in both panes)
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Left):
		m.pane = paneLeft
		return m, nil

	case key.Matches(msg, keys.Right):
		if m.focusedStack != nil {
			m.pane = paneRight
			m.selectedBranchIdx = len(m.focusedStack.Stack.AllBranches) - 1
		}
		return m, nil
	}

	// Pane-specific key handling
	switch m.pane {
	case paneRight:
		return m.handleRightPaneKey(msg)
	default:
		return m.handleLeftPaneKey(msg)
	}
}

// handleLeftPaneKey handles keyboard input when the left (stacks) pane is focused.
func (m *shippableModel) handleLeftPaneKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up):
		if m.selectedIndex > 0 {
			m.selectedIndex--
			m.updateFocusedStack()
		}
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.selectedIndex < len(m.stacks)-1 {
			m.selectedIndex++
			m.updateFocusedStack()
		}
		return m, nil

	case key.Matches(msg, keys.Select):
		m.toggleSelection()
		// Clear previous combination result and trigger background analysis
		m.combination = nil
		return m, m.backgroundAnalyze()

	case key.Matches(msg, keys.Expand):
		m.toggleExpand()
		return m, nil

	case key.Matches(msg, keys.SelectAll):
		m.selectAllShippable()
		// Clear previous combination result and trigger background analysis
		m.combination = nil
		return m, m.backgroundAnalyze()

	case key.Matches(msg, keys.Ship):
		return m.startShip()

	case key.Matches(msg, keys.Submit):
		return m.startSubmit()

	case key.Matches(msg, keys.Analyze):
		return m.startCombinationAnalysis()

	case key.Matches(msg, keys.Refresh):
		m.state = stateLoading
		m.statusMessage = msgRefreshing
		return m, m.refresh()

	case key.Matches(msg, keys.Help):
		m.state = stateHelp
		return m, nil
	}

	return m, nil
}

// handleRightPaneKey handles keyboard input when the right (details) pane is focused.
func (m *shippableModel) handleRightPaneKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up):
		if m.focusedStack != nil && m.selectedBranchIdx < len(m.focusedStack.Stack.AllBranches)-1 {
			m.selectedBranchIdx++
		}
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.selectedBranchIdx > 0 {
			m.selectedBranchIdx--
		}
		return m, nil

	case key.Matches(msg, keys.Help):
		m.state = stateHelp
		return m, nil

	case key.Matches(msg, keys.Refresh):
		m.state = stateLoading
		m.statusMessage = msgRefreshing
		return m, m.refresh()

	case key.Matches(msg, keys.Submit):
		return m.startSubmit()

	case key.Matches(msg, keys.Squash):
		return m.startSquash()
	}

	// Forward unhandled keys to the viewport for pgup/pgdown scrolling
	var cmd tea.Cmd
	m.detailsViewport, cmd = m.detailsViewport.Update(msg)
	return m, cmd
}

// handleConfirmationKey handles keyboard input during confirmation dialog.
func (m *shippableModel) handleConfirmationKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Confirm):
		m.state = stateMain
		switch m.confirmAction {
		case confirmActionShip:
			return m.executeShip()
		case confirmActionSquash:
			return m.executeSquash()
		}
		return m, nil

	case key.Matches(msg, keys.Cancel):
		m.state = stateMain
		m.confirmAction = ""
		m.confirmBranch = ""
		return m, nil
	}
	return m, nil
}

// updateFocusedStack updates the focused stack based on selection.
func (m *shippableModel) updateFocusedStack() {
	m.focusedStack = m.currentStack()
}

// refresh fetches the latest stack analysis.
func (m *shippableModel) refresh() tea.Cmd {
	return func() tea.Msg {
		// Rebuild engine cache to pick up external changes (e.g., move operations in another terminal)
		if err := m.engine.Rebuild(m.engine.Trunk().GetName()); err != nil {
			return refreshCompleteMsg{err: err}
		}
		result, err := m.analyzer.AnalyzeAll(m.ctx.Context)
		return refreshCompleteMsg{result: result, err: err}
	}
}

// startShip initiates the ship workflow.
func (m *shippableModel) startShip() (tea.Model, tea.Cmd) {
	selected := m.selectedStacks()
	if len(selected) == 0 {
		m.errorMessage = "No stacks selected"
		return m, nil
	}

	// Check if all selected stacks are shippable
	for _, s := range selected {
		if !s.IsShippable() {
			m.errorMessage = "Cannot ship: " + s.RootBranch() + " is not shippable"
			return m, nil
		}
	}

	// Show confirmation dialog
	m.state = stateConfirming
	m.confirmAction = confirmActionShip
	return m, nil
}

// shipCompleteMsg signals that the ship operation completed.
type shipCompleteMsg struct {
	result *shippable.ShipResult
	err    error
}

// executeShip executes the ship action after confirmation.
func (m *shippableModel) executeShip() (tea.Model, tea.Cmd) {
	selected := m.selectedStacks()
	if len(selected) == 0 {
		m.errorMessage = "No stacks selected"
		return m, nil
	}

	m.state = stateShipping
	m.statusMessage = "Shipping..."

	return m, func() tea.Msg {
		quietCtx := m.quietCtx()
		shipper := shippable.NewShipper(quietCtx)
		result, err := shipper.Ship(selected, shippable.ShipOptions{
			SkipLocalCI: !m.options.RunLocalCI,
			Wait:        false, // Don't wait in UI, user can monitor PR
		})
		return shipCompleteMsg{result: result, err: err}
	}
}

// startCombinationAnalysis checks if selected stacks can be combined (blocking UI).
func (m *shippableModel) startCombinationAnalysis() (tea.Model, tea.Cmd) {
	selected := m.selectedStacks()
	if len(selected) == 0 {
		m.errorMessage = "No stacks selected for analysis"
		return m, nil
	}

	m.state = stateAnalyzing
	m.statusMessage = "Analyzing combination..."

	return m, func() tea.Msg {
		result, err := m.combiner.CheckCombination(m.ctx.Context, selected, shippable.CheckCombinationOptions{
			RunLocalCI: m.options.RunLocalCI,
		})
		return combinationCompleteMsg{result: result, err: err}
	}
}

// backgroundAnalyze runs combination analysis in the background without blocking UI.
// Returns nil cmd if there are fewer than 2 stacks selected (nothing to analyze).
func (m *shippableModel) backgroundAnalyze() tea.Cmd {
	// Only analyze if we have 2+ selected stacks
	if m.cache.selectedCount < 2 {
		return nil
	}

	// Use cached selected stacks to avoid iterating again
	selected := m.cache.selectedStacks

	return func() tea.Msg {
		result, err := m.combiner.CheckCombination(m.ctx.Context, selected, shippable.CheckCombinationOptions{
			RunLocalCI: m.options.RunLocalCI,
		})
		return combinationCompleteMsg{result: result, err: err}
	}
}

// startSquash initiates the squash workflow for the selected branch in the right pane.
func (m *shippableModel) startSquash() (tea.Model, tea.Cmd) {
	stack := m.focusedStack
	if stack == nil {
		m.errorMessage = "No stack focused"
		return m, nil
	}

	if m.selectedBranchIdx < 0 || m.selectedBranchIdx >= len(stack.Stack.AllBranches) {
		m.errorMessage = "No branch selected"
		return m, nil
	}

	branchName := stack.Stack.AllBranches[m.selectedBranchIdx]

	// Don't allow squashing trunk
	if m.engine.IsTrunk(m.engine.GetBranch(branchName)) {
		m.errorMessage = "Cannot squash trunk"
		return m, nil
	}

	m.state = stateConfirming
	m.confirmAction = confirmActionSquash
	m.confirmBranch = branchName
	return m, nil
}

// executeSquash runs the squash action after confirmation.
func (m *shippableModel) executeSquash() (tea.Model, tea.Cmd) {
	branchName := m.confirmBranch
	m.confirmBranch = ""
	m.confirmAction = ""

	if branchName == "" {
		m.errorMessage = "No branch to squash"
		return m, nil
	}

	m.state = stateSquashing
	m.statusMessage = "Squashing " + branchName + "..."

	return m, func() tea.Msg {
		quietCtx := m.quietCtx()

		// Save current branch to restore after squash
		currentBranch := m.engine.CurrentBranch()

		// Checkout the target branch
		targetBranch := m.engine.GetBranch(branchName)
		if err := m.engine.CheckoutBranch(quietCtx.Context, targetBranch); err != nil {
			return squashCompleteMsg{branch: branchName, err: fmt.Errorf("checkout %s: %w", branchName, err)}
		}

		// Run squash
		if err := actions.SquashAction(quietCtx, actions.SquashOptions{NoEdit: true}); err != nil {
			// Restore original branch on error
			if currentBranch != nil {
				_ = m.engine.CheckoutBranch(quietCtx.Context, *currentBranch)
			}
			return squashCompleteMsg{branch: branchName, err: err}
		}

		// Restore original branch
		if currentBranch != nil {
			_ = m.engine.CheckoutBranch(quietCtx.Context, *currentBranch)
		}

		return squashCompleteMsg{branch: branchName}
	}
}

// startSubmit initiates the restack + submit workflow for the focused stack.
func (m *shippableModel) startSubmit() (tea.Model, tea.Cmd) {
	stack := m.focusedStack
	if stack == nil {
		m.errorMessage = "No stack focused"
		return m, nil
	}

	root := stack.RootBranch()
	m.state = stateSubmitting
	m.statusMessage = "Submitting " + root + "..."

	return m, func() tea.Msg {
		opts := submit.Options{
			Branch:     root,
			StackRange: engine.StackRangeFull(),
			Confirm:    true, // Skip confirmation
			Restack:    true, // Restack before submitting
		}

		submitted := 0
		handler := &dashboardSubmitHandler{onSubmit: func() { submitted++ }}
		if err := submit.Action(m.quietCtx(), opts, handler); err != nil {
			return submitCompleteMsg{err: err}
		}

		return submitCompleteMsg{submitted: submitted}
	}
}

// dashboardSubmitHandler is a minimal handler for submit events in the dashboard.
type dashboardSubmitHandler struct {
	onSubmit func()
}

func (h *dashboardSubmitHandler) OnEvent(event submit.Event) {
	if e, ok := event.(submit.BranchProgressEvent); ok && e.Status == submit.StatusDone {
		h.onSubmit()
	}
}

func (h *dashboardSubmitHandler) Confirm(_ string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

func (h *dashboardSubmitHandler) IsInteractive() bool {
	return false
}
