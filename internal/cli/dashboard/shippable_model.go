package dashboard

import (
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/shippable"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/core"
)

const (
	// autoRefreshInterval is the time between automatic refreshes
	autoRefreshInterval = 30 * time.Second
	// tickInterval is the interval for timer updates (for countdown display)
	// Using 5s instead of 1s to reduce render frequency while still showing useful countdown
	tickInterval = 5 * time.Second

	// msgRefreshing is the status message shown during refresh operations.
	msgRefreshing = "Refreshing..."

	// confirmActionShip is the confirm action for shipping stacks.
	confirmActionShip = "ship"
	// confirmActionSquash is the confirm action for squashing a branch.
	confirmActionSquash = "squash"
)

// renderCache stores precomputed data to avoid expensive git operations in the render loop.
// This cache is rebuilt only on refresh, not on every render.
type renderCache struct {
	// stackTitles maps stack root branch to the display title (computed from commit subject)
	stackTitles map[string]string

	// stackDescriptions maps stack root branch to the stack description (from metadata)
	stackDescriptions map[string]*git.StackDescription

	// branchAnnotations maps branch name to its tree annotation
	branchAnnotations map[string]tree.BranchAnnotation

	// treeRenderer is the pre-built tree renderer, reused across renders
	treeRenderer *tree.StackTreeRenderer

	// currentBranch is the name of the currently checked-out branch
	currentBranch string
	// currentStackRoot is the root branch of the stack containing the checked-out branch
	currentStackRoot string

	// branchBlocking maps branch name to its blocking reason (only for blocked branches)
	branchBlocking map[string]shippable.BlockingReason

	// commitBodies maps stack root branch to the commit body (for single-branch stacks
	// without a stack description, used as fallback description)
	commitBodies map[string]string

	// Cached selection state to avoid recomputing
	selectedCount  int
	selectedStacks []shippable.Stack
}

// focusedPane indicates which pane has keyboard focus.
type focusedPane int

const (
	paneLeft focusedPane = iota
	paneRight
)

// dashboardState represents the current UI state of the dashboard.
type dashboardState int

const (
	stateLoading dashboardState = iota
	stateMain
	stateAnalyzing
	stateConfirming
	stateShipping
	stateSubmitting
	stateSquashing
	stateHelp
)

// ShippableOptions defines configuration for the shippable dashboard.
type ShippableOptions struct {
	// RunLocalCI enables local CI validation during combination checks.
	RunLocalCI bool
}

// shippableModel is the main model for the shippable work dashboard.
type shippableModel struct {
	core.BaseModel // Provides Ready signaling, spinner, and common message handling

	ctx    *app.Context
	engine engine.Engine
	cfg    config.Configurer

	// Analysis state
	analyzer    *shippable.Analyzer
	combiner    *shippable.Combiner
	analysis    *shippable.AnalysisResult
	combination *shippable.CombinationResult

	// UI state
	state             dashboardState // Current UI state (replaces boolean flags)
	stacks            []shippable.Stack
	selectedIndex     int
	expanded          map[string]bool  // Tracks which stacks are expanded
	selected          map[string]bool  // Tracks which stacks are selected for shipping
	locked            map[string]bool  // Tracks which stacks are locked (during publish/ship)
	focusedStack      *shippable.Stack // Currently focused stack for detail view
	pane              focusedPane      // Which pane has keyboard focus
	selectedBranchIdx int              // Index of highlighted branch in the details tree
	detailsViewport   viewport.Model   // Viewport for scrollable details panel

	// Status
	initialLoad   bool // true until first refresh completes
	lastRefresh   time.Time
	statusMessage string
	errorMessage  string

	// Progress tracking
	progress        progress.Model
	progressStep    int
	progressTotal   int
	progressMessage string

	// Confirmation action (used when state == stateConfirming)
	confirmAction string
	confirmBranch string // target branch for squash confirmation

	// Options
	options ShippableOptions

	// Render cache - rebuilt on refresh to avoid git operations in render loop
	cache renderCache
}

// keyMap defines all keyboard shortcuts for the dashboard.
type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Left      key.Binding
	Right     key.Binding
	Select    key.Binding
	Expand    key.Binding
	Ship      key.Binding
	Submit    key.Binding
	Analyze   key.Binding
	Refresh   key.Binding
	Help      key.Binding
	Quit      key.Binding
	Squash    key.Binding
	Confirm   key.Binding
	Cancel    key.Binding
	SelectAll key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys(core.KeyUp, "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys(core.KeyDown, "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Left: key.NewBinding(
		key.WithKeys(core.KeyLeft, "h"),
		key.WithHelp("←/h", "focus stacks"),
	),
	Right: key.NewBinding(
		key.WithKeys(core.KeyRight, "l"),
		key.WithHelp("→/l", "focus details"),
	),
	Select: key.NewBinding(
		key.WithKeys("space", " "),
		key.WithHelp("space", "toggle selection"),
	),
	Expand: key.NewBinding(
		key.WithKeys(core.KeyEnter),
		key.WithHelp("enter", "expand/collapse"),
	),
	Ship: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "ship selected"),
	),
	Submit: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "submit stack"),
	),
	Analyze: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "analyze combination"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Quit: key.NewBinding(
		key.WithKeys(core.KeyQuit, core.KeyCtrlC),
		key.WithHelp("q", "quit"),
	),
	Squash: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "squash branch"),
	),
	Confirm: key.NewBinding(
		key.WithKeys(core.KeyEnter, "y"),
		key.WithHelp("enter/y", "confirm"),
	),
	Cancel: key.NewBinding(
		key.WithKeys(core.KeyEsc, "n"),
		key.WithHelp("esc/n", "cancel"),
	),
	SelectAll: key.NewBinding(
		key.WithKeys("A"),
		key.WithHelp("A", "select all shippable"),
	),
}

// newShippableModel creates a new shippable dashboard model.
func newShippableModel(ctx *app.Context, cfg config.Configurer, opts ShippableOptions) *shippableModel {
	analyzer := shippable.NewAnalyzer(ctx.Engine, ctx.GitHub())
	combiner := shippable.NewCombiner(ctx.Engine, cfg, output.NewNullOutput())

	// Create progress bar
	p := progress.New(
		progress.WithDefaultBlend(),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)

	// Create viewport for the details panel with default keybindings,
	// but disable up/down/left/right since we handle those for branch selection.
	vp := viewport.New()
	vp.KeyMap.Up.SetEnabled(false)
	vp.KeyMap.Down.SetEnabled(false)
	vp.KeyMap.Left.SetEnabled(false)
	vp.KeyMap.Right.SetEnabled(false)
	vp.MouseWheelEnabled = true

	return &shippableModel{
		ctx:             ctx,
		engine:          ctx.Engine,
		cfg:             cfg,
		analyzer:        analyzer,
		combiner:        combiner,
		expanded:        make(map[string]bool),
		selected:        make(map[string]bool),
		locked:          make(map[string]bool),
		initialLoad:     true,
		state:           stateLoading,
		options:         opts,
		progress:        p,
		detailsViewport: vp,
	}
}

// selectedStacks returns the stacks that are currently selected.
func (m *shippableModel) selectedStacks() []shippable.Stack {
	var result []shippable.Stack
	for _, s := range m.stacks {
		if m.selected[s.RootBranch()] {
			result = append(result, s)
		}
	}
	return result
}

// quietCtx returns a copy of the app context with output silenced.
// Use this for actions running inside tea.Cmd to prevent stdout writes
// from conflicting with the running bubbletea program.
func (m *shippableModel) quietCtx() *app.Context {
	ctx := *m.ctx //nolint:govet // Copying Context by value is intentional when silencing output for background TUI actions.
	ctx.Output = output.NewNullOutput()
	return &ctx
}

// currentStack returns the currently focused stack, or nil if none.
func (m *shippableModel) currentStack() *shippable.Stack {
	if m.selectedIndex >= 0 && m.selectedIndex < len(m.stacks) {
		return &m.stacks[m.selectedIndex]
	}
	return nil
}

// isLocked returns true if the stack is currently locked (during publish/ship).
func (m *shippableModel) isLocked(rootBranch string) bool {
	return m.locked[rootBranch]
}

// toggleSelection toggles selection of the current stack.
// Shippable and pending stacks can be selected.
// Locked, blocked, and incomplete stacks cannot be selected.
func (m *shippableModel) toggleSelection() {
	stack := m.currentStack()
	if stack == nil {
		return
	}

	root := stack.RootBranch()

	// Locked stacks cannot be selected
	if m.isLocked(root) {
		return
	}

	// Only shippable and pending stacks can be selected
	if !stack.IsShippable() && !stack.IsPending() {
		return
	}

	m.selected[root] = !m.selected[root]
	m.updateSelectionCache()
}

// toggleExpand toggles expansion of the current stack.
func (m *shippableModel) toggleExpand() {
	stack := m.currentStack()
	if stack == nil {
		return
	}
	root := stack.RootBranch()
	m.expanded[root] = !m.expanded[root]
}

// selectAllShippable selects all shippable and pending stacks that are not locked.
func (m *shippableModel) selectAllShippable() {
	for _, s := range m.stacks {
		root := s.RootBranch()
		if (s.IsShippable() || s.IsPending()) && !m.isLocked(root) {
			m.selected[root] = true
		}
	}
	m.updateSelectionCache()
}

// clearSelection clears all selections.
func (m *shippableModel) clearSelection() {
	m.selected = make(map[string]bool)
	m.updateSelectionCache()
}

// updateSelectionCache recalculates the cached selection state.
// Call this whenever m.selected changes.
func (m *shippableModel) updateSelectionCache() {
	m.cache.selectedCount = 0
	m.cache.selectedStacks = nil
	for _, s := range m.stacks {
		if m.selected[s.RootBranch()] {
			m.cache.selectedCount++
			m.cache.selectedStacks = append(m.cache.selectedStacks, s)
		}
	}
}

// rebuildCache rebuilds all cached render data.
// Call this after stacks are updated (on refresh).
func (m *shippableModel) rebuildCache() {
	// Initialize maps
	m.cache.stackTitles = make(map[string]string)
	m.cache.stackDescriptions = make(map[string]*git.StackDescription)
	m.cache.branchAnnotations = make(map[string]tree.BranchAnnotation)
	m.cache.branchBlocking = make(map[string]shippable.BlockingReason)
	m.cache.commitBodies = make(map[string]string)

	// Determine which stack contains the currently checked-out branch
	m.cache.currentBranch = ""
	m.cache.currentStackRoot = ""
	if current := m.engine.CurrentBranch(); current != nil {
		m.cache.currentBranch = current.GetName()
		for _, stack := range m.stacks {
			if slices.Contains(stack.Stack.AllBranches, m.cache.currentBranch) {
				m.cache.currentStackRoot = stack.RootBranch()
			}
			if m.cache.currentStackRoot != "" {
				break
			}
		}
	}

	// Precompute titles, descriptions, and annotations for all stacks
	for _, stack := range m.stacks {
		rootBranch := stack.RootBranch()

		// Use the title the analyzer already computed (PR title > commit subject > branch name)
		// rather than recomputing from commit subject alone, which both discards a real PR
		// title when one exists and re-spawns git to redo work the analyzer already did.
		m.cache.stackTitles[rootBranch] = stack.DisplayTitle()

		// Get stack description from metadata
		if branch := m.engine.GetBranch(rootBranch); branch.GetName() != "" {
			if desc := m.engine.GetStackDescription(branch); desc != nil && !desc.IsEmpty() {
				m.cache.stackDescriptions[rootBranch] = desc
			} else if stack.BranchCount() == 1 {
				// For single-branch stacks without a description, use commit body
				if body := branch.DefaultPRBody(); body != "" {
					m.cache.commitBodies[rootBranch] = strings.TrimSpace(body)
				}
			}
		}

		// Compute annotations for all branches in the stack, reading their
		// git-computed stats from batched value maps rather than per branch (this
		// runs on every refresh and on the auto-refresh timer). The view renders
		// with ShowCommitMessages, so resolve commit messages once via BatchCommits
		// and skip the per-branch GetAllCommits inside GetBranchAnnotation.
		stackBranches := engine.Branches{}
		for _, branchName := range stack.Stack.AllBranches {
			if branch := m.engine.GetBranch(branchName); branch.GetName() != "" {
				stackBranches = stackBranches.Append(branch)
			}
		}
		stats := m.engine.BatchBranchStats(stackBranches)
		commits := m.engine.BatchCommits(stackBranches, engine.CommitFormatReadable)
		for _, branch := range stackBranches {
			name := branch.GetName()
			ann := tui.GetBranchAnnotation(m.engine, branch, stats[name], tui.AnnotationOptions{SkipCommitMessages: true})
			if msgs := commits[name]; msgs != nil {
				ann.CommitMessages = msgs
				ann.CommitCount = len(msgs)
			}
			m.cache.branchAnnotations[name] = ann
		}

		// Cache blocking reasons per branch
		for _, bp := range stack.BlockingPRs {
			m.cache.branchBlocking[bp.Branch] = bp.Reason
		}
	}

	// Build a single tree renderer that can be reused
	// This avoids rebuilding the stack graph on every render
	m.cache.treeRenderer = tui.NewStackTreeRenderer(m.engine)

	// Update selection cache
	m.updateSelectionCache()
}

// Init initializes the model.
func (m *shippableModel) Init() tea.Cmd {
	m.SignalReady()
	return tea.Batch(m.InitSpinner(), m.refresh(), m.tick())
}

// tick returns a command that sends a tick message after the tick interval.
func (m *shippableModel) tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// HandleSpinnerMsg handles spinner tick messages for loading animations.
// Returns (handled, cmd) - if handled is true, the caller should return cmd.
func (m *shippableModel) HandleSpinnerMsg(msg tea.Msg) (bool, tea.Cmd) {
	// Use the BaseModel's spinner handling
	return m.HandleCommonMsg(msg)
}

// Messages for async operations
type (
	analysisCompleteMsg struct {
		result *shippable.AnalysisResult
		err    error
	}

	combinationCompleteMsg struct {
		result *shippable.CombinationResult
		err    error
	}

	refreshCompleteMsg struct {
		result *shippable.AnalysisResult
		err    error
	}

	submitCompleteMsg struct {
		submitted int
		err       error
	}

	squashCompleteMsg struct {
		branch string
		err    error
	}

	// progressUpdateMsg updates the progress bar during async operations
	progressUpdateMsg struct {
		step    int
		total   int
		message string
	}

	// tickMsg is sent periodically for countdown updates and auto-refresh
	tickMsg time.Time
)
