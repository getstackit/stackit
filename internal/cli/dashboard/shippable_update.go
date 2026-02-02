package dashboard

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/actions/submit"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/shippable"
)

// Update handles all messages and updates the model.
func (m *shippableModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle window size messages
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width = wsMsg.Width
		m.Height = wsMsg.Height
		// Also update progress bar width
		m.progress.Width = min(wsMsg.Width-20, 60)
		return m, nil
	}

	// Handle spinner ticks for loading animations
	if handled, cmd := m.HandleSpinnerMsg(msg); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
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
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
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

	case publishCompleteMsg:
		m.state = stateMain
		m.progressStep = 0
		m.progressTotal = 0
		m.progressMessage = ""
		m.unlockAllStacks()
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return m, nil
		}
		m.statusMessage = fmt.Sprintf("Published! Restacked %d, submitted %d branches", msg.restacked, msg.submitted)
		return m, m.refresh()
	}

	return m, nil
}

// handleKeyMsg handles keyboard input.
func (m *shippableModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If showing confirmation, handle confirm/cancel
	if m.state == stateConfirming {
		return m.handleConfirmationKey(msg)
	}

	// If showing help, any key closes it
	if m.state == stateHelp {
		m.state = stateMain
		return m, nil
	}

	// If loading, analyzing, shipping, or publishing, only allow quit
	if m.state == stateLoading || m.state == stateAnalyzing || m.state == stateShipping || m.state == statePublishing {
		if key.Matches(msg, keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

	// Handle normal navigation and actions
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

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

	case key.Matches(msg, keys.Publish):
		return m.startPublish()

	case key.Matches(msg, keys.Analyze):
		return m.startCombinationAnalysis()

	case key.Matches(msg, keys.Refresh):
		m.state = stateLoading
		m.statusMessage = "Refreshing..."
		return m, m.refresh()

	case key.Matches(msg, keys.Help):
		m.state = stateHelp
		return m, nil
	}

	return m, nil
}

// handleConfirmationKey handles keyboard input during confirmation dialog.
func (m *shippableModel) handleConfirmationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Confirm):
		m.state = stateMain
		if m.confirmAction == "ship" {
			return m.executeShip()
		}
		return m, nil

	case key.Matches(msg, keys.Cancel):
		m.state = stateMain
		m.confirmAction = ""
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
	m.confirmAction = "ship"
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
		shipper := shippable.NewShipper(m.ctx)
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

// startPublish initiates the restack + submit workflow for all branches.
func (m *shippableModel) startPublish() (tea.Model, tea.Cmd) {
	m.state = statePublishing
	m.statusMessage = "Restacking and submitting..."
	m.lockAllStacks()

	return m, func() tea.Msg {
		eng := m.engine
		ctx := m.ctx

		// Get all branches that need restacking
		graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
		allBranches := graph.Range(eng.Trunk(), engine.StackRange{RecursiveChildren: true})

		// Restack all branches
		sortedBranches := eng.SortBranchesTopologically(allBranches)
		restacked := 0

		if len(sortedBranches) > 0 {
			err := actions.RestackBranchesWithHandler(ctx, sortedBranches, func(_ string, result engine.RestackResult, _ string, _ bool, _ engine.LockReason, _ bool, _ bool, _ bool, _, _ string) {
				if result == engine.RestackDone {
					restacked++
				}
			}, false)
			if err != nil {
				return publishCompleteMsg{err: err}
			}
		}

		// Submit all branches
		opts := submit.Options{
			Branch:     eng.CurrentBranch().GetName(),
			StackRange: engine.StackRangeFull(),
			Confirm:    true, // Skip confirmation
		}

		submitted := 0
		handler := &publishSubmitHandler{onSubmit: func() { submitted++ }}
		if err := submit.Action(ctx, opts, handler); err != nil {
			return publishCompleteMsg{restacked: restacked, err: err}
		}

		return publishCompleteMsg{restacked: restacked, submitted: submitted}
	}
}

// publishSubmitHandler is a minimal handler for submit events during publish.
type publishSubmitHandler struct {
	onSubmit func()
}

func (h *publishSubmitHandler) OnEvent(event submit.Event) {
	if e, ok := event.(submit.BranchProgressEvent); ok && e.Status == submit.StatusDone {
		h.onSubmit()
	}
}

func (h *publishSubmitHandler) Confirm(_ string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

func (h *publishSubmitHandler) IsInteractive() bool {
	return false
}
