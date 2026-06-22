package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/keys"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

const (
	logStyleFull = "FULL"

	// validationDebounceTime is the delay before triggering validation after selection changes
	validationDebounceTime = 300 * time.Millisecond
)

// TreeMode defines how the log is used
type TreeMode int

const (
	// TreeModeView is the default view mode for browsing the log
	TreeModeView TreeMode = iota
	// TreeModeSelect is the selection mode for choosing a branch
	TreeModeSelect
)

// TreeModel is the bubbletea model for the interactive log
type TreeModel struct {
	context      context.Context
	engine       engine.Engine
	githubClient github.Client
	renderer     *tree.StackTreeRenderer
	allBranches  []engine.Branch
	trunkName    string
	viewport     viewport.Model
	width        int
	height       int
	ready        bool
	logger       output.Logger

	// Keys
	treeKeys   keys.TreeKeyMap
	selectKeys keys.SelectKeyMap

	// State
	mode           TreeMode
	branches       []tree.RenderedBranch // Visible branches with their lines
	selectedIndex  int
	selectedBranch string
	collapsed      map[string]bool
	canceled       bool

	// Search state
	searchQuery   string
	inSearchMode  bool
	searchMatches map[string]bool // Branch name -> whether it matches search

	// Cached rendering - two-phase approach for fast navigation
	cachedTreeData  *tree.CachedTreeData // Cached tree without selection (Phase 3)
	cachedTreeValid bool                 // Whether cachedTreeData is valid
	cachedLines     []string             // All rendered lines (with selection applied)
	branchLineStart []int                // Starting line offset for each rendered branch

	// Options
	altScreen         bool
	style             string
	showUntracked     bool
	exclude           map[string]bool
	nonSelectable     map[string]bool // Branches visible but cursor skips them
	header            string          // Custom header text for selection mode
	skipEnrichment    bool            // Skip background GitHub/git enrichment
	inline            bool            // Run inline without alt-screen
	validateSelection func(branchName string) *SelectionValidation

	// Validation state
	validationResult    *SelectionValidation // Current validation result
	validationPending   bool                 // Whether validation is pending (debounce)
	lastValidatedBranch string               // Branch that was last validated
}

// NewTreeModel creates a new TreeModel
func NewTreeModel(ctx context.Context, eng engine.Engine, ghClient github.Client, opts TreeOptions) *TreeModel {
	logger := opts.Logger
	treeDebug := func(msg string, args ...any) {
		if logger != nil {
			logger.Debug(msg, args...)
		}
	}

	initStart := time.Now()
	treeDebug("NewTreeModel started")

	// Build filter function
	var filter func(string) bool
	if len(opts.Exclude) > 0 {
		filter = func(name string) bool {
			return !opts.Exclude[name]
		}
	}

	// Detect worktrees (builds both empty and stack-root maps in one call)
	start := time.Now()
	wtData := GetWorktreeData(eng)
	var emptyWorktreeNames map[string]bool
	if len(wtData.EmptyWorktrees) > 0 {
		emptyWorktreeNames = make(map[string]bool)
		for name := range wtData.EmptyWorktrees {
			emptyWorktreeNames[name] = true
		}
	}
	treeDebug("GetWorktreeData completed in %v, found %d empty worktrees", time.Since(start), len(wtData.EmptyWorktrees))

	// Create renderer synchronously for instant display
	start = time.Now()
	renderer := NewStackTreeRendererWithOptions(eng, engine.SortStrategySmart, filter, emptyWorktreeNames)
	treeDebug("NewStackTreeRendererWithOptions completed in %v", time.Since(start))

	// Build minimal annotations synchronously (includes worktree info, no git/network calls)
	start = time.Now()
	allBranches := eng.AllBranches()
	annotations := make(map[string]tree.BranchAnnotation)
	for _, b := range allBranches {
		annotations[b.GetName()] = GetMinimalAnnotationWithWorktreeAndEmpty(eng, b, wtData)
	}
	// Apply annotation overrides (e.g., custom labels for move operation)
	if opts.AnnotationOverrides != nil {
		for name, override := range opts.AnnotationOverrides {
			ann := annotations[name]
			if override.CustomLabel != "" {
				ann.CustomLabel = override.CustomLabel
			}
			annotations[name] = ann
		}
	}
	renderer.SetAnnotations(annotations)
	treeDebug("Minimal annotations with worktree completed in %v", time.Since(start))

	// Set initial selection
	start = time.Now()
	trunkName := eng.Trunk().GetName()
	selectedBranch := ""
	if current := eng.CurrentBranch(); current != nil {
		selectedBranch = current.GetName()
	} else {
		selectedBranch = trunkName
	}
	treeDebug("Initial selection completed in %v", time.Since(start))

	// Initialize search matches (all branches match when no search query)
	searchMatches := make(map[string]bool)
	for _, b := range allBranches {
		searchMatches[b.GetName()] = true
	}

	treeDebug("NewTreeModel completed in %v", time.Since(initStart))

	m := &TreeModel{
		context:           ctx,
		engine:            eng,
		githubClient:      ghClient,
		logger:            opts.Logger,
		renderer:          renderer,
		allBranches:       allBranches,
		trunkName:         trunkName,
		selectedBranch:    selectedBranch,
		treeKeys:          keys.DefaultTree,
		selectKeys:        keys.DefaultSelect,
		style:             opts.Style,
		showUntracked:     opts.ShowUntracked,
		exclude:           opts.Exclude,
		nonSelectable:     opts.NonSelectable,
		header:            opts.Header,
		skipEnrichment:    opts.SkipEnrichment,
		inline:            opts.Inline,
		validateSelection: opts.ValidateSelection,
		collapsed:         make(map[string]bool),
		searchMatches:     searchMatches,
		mode:              TreeModeView,
	}

	return m
}

// newTreeSelectModel creates a new TreeModel in selection mode
func newTreeSelectModel(ctx context.Context, eng engine.Engine, ghClient github.Client, opts TreeOptions) *TreeModel {
	m := NewTreeModel(ctx, eng, ghClient, opts)
	m.mode = TreeModeSelect
	return m
}

// SetAltScreen sets whether the model should use alt screen mode
func (m *TreeModel) SetAltScreen(enabled bool) {
	m.altScreen = enabled
}

// Init initializes the bubbletea model
func (m *TreeModel) Init() tea.Cmd {
	// For inline mode, pre-render the tree immediately since we won't wait for WindowSizeMsg
	if m.inline {
		m.renderTree()
		// Find initial selectedIndex
		for i, b := range m.branches {
			if b.Name == m.selectedBranch {
				m.selectedIndex = i
				break
			}
		}
	}

	// Renderer is already created with minimal data in NewTreeModel.
	// Skip enrichment if requested (e.g., for checkout where GitHub data isn't needed).
	if m.skipEnrichment {
		return nil
	}
	// Run enrichment in the background.
	return m.enrichData()
}

// log logs a message if logger is available
func (m *TreeModel) log(msg string, args ...any) {
	if m.logger != nil {
		m.logger.Debug(msg, args...)
	}
}

// enrichData returns a command that fetches full annotation data in the background.
// This includes git operations and CI status network calls.
func (m *TreeModel) enrichData() tea.Cmd {
	// Capture values needed by the goroutine to avoid races on struct fields
	ctx := m.context
	eng := m.engine
	ghClient := m.githubClient
	allBranches := m.allBranches
	style := m.style
	logger := m.logger

	treeDebug := func(msg string, args ...any) {
		if logger != nil {
			logger.Debug(msg, args...)
		}
	}

	logError := func(msg string, args ...any) {
		if logger != nil {
			logger.Error(msg, args...)
		}
	}

	// Wrap with panic recovery
	return SafeCmdFunc("TUI enrichment", logger, func() tea.Msg {
		enrichStart := time.Now()
		treeDebug("TUI enrichment started")

		// Channels for parallel results (buffered so goroutines don't block)
		type ciResult struct {
			statuses map[string]*github.CheckStatus
			err      error
		}
		ciChan := make(chan ciResult, 1)
		if style == logStyleFull && ghClient != nil {
			branchNames := engine.Branches(allBranches).Select(engine.BranchFilter{ExcludeTrunk: true, RequirePR: true}).Names()
			if len(branchNames) > 0 {
				go func() {
					defer func() {
						if p := recover(); p != nil {
							logError("BatchGetPRChecksStatus panicked: %v", p)
							ciChan <- ciResult{err: fmt.Errorf("panicked: %v", p)}
							return
						}
					}()
					start := time.Now()
					statuses, err := ghClient.BatchGetPRChecksStatus(ctx, branchNames)
					if err != nil {
						logError("BatchGetPRChecksStatus failed: %v", err)
					}
					treeDebug("BatchGetPRChecksStatus for %d branches completed in %v", len(branchNames), time.Since(start))
					ciChan <- ciResult{statuses: statuses, err: err}
				}()
			} else {
				ciChan <- ciResult{}
			}
		} else {
			ciChan <- ciResult{}
		}

		// Wait for CI status fetch to complete
		ciRes := <-ciChan
		if ciRes.err != nil {
			logError("CI status fetch failed, skipping CI annotations: %v", ciRes.err)
		}
		ciStatuses := ciRes.statuses

		// Detect worktrees (builds both empty and stack-root maps in one call)
		wtData := GetWorktreeData(eng)

		// Resolve the git-computed annotation stats (short SHA, commit count, diff
		// stats) for all branches as one batched value, instead of warming the
		// engine-global caches. Forge status (CI) and worktree data above are
		// separate concerns, joined into the annotation below.
		stats := eng.BatchBranchStats(allBranches)

		// Collect full annotations
		start := time.Now()
		enrichment := &AnnotationEnrichment{
			CIStatuses:          ciStatuses,
			EmptyWorktrees:      wtData.EmptyWorktrees,
			WorktreeByStackRoot: wtData.WorktreeByStackRoot,
		}
		// Build into a positional slice so concurrent workers don't race on a
		// shared map, then assemble the map serially.
		built := make([]tree.BranchAnnotation, len(allBranches))
		utils.Run(indexedBranches(allBranches), func(item indexedBranch) {
			built[item.index] = BuildFullAnnotation(eng, item.branch, stats[item.branch.GetName()], enrichment, AnnotationOptions{
				SkipCommitMessages: true,
			})
		})
		annotations := make(map[string]tree.BranchAnnotation, len(allBranches))
		for i, b := range allBranches {
			annotations[b.GetName()] = built[i]
		}
		treeDebug("Collected full annotations for %d branches in %v", len(allBranches), time.Since(start))

		treeDebug("TUI enrichment completed in %v", time.Since(enrichStart))

		return enrichDataMsg{annotations: annotations}
	})
}

// enrichDataMsg is sent when full annotation data (including git/network) is ready
type enrichDataMsg struct {
	annotations map[string]tree.BranchAnnotation
}

// invalidateTreeCache marks the cached tree data as stale.
// Call this when the tree structure or display changes (collapse, search, data enrichment).
// Navigation (up/down) does NOT invalidate the cache - it uses the fast path.
func (m *TreeModel) invalidateTreeCache() {
	m.cachedTreeValid = false
}

// validationTickMsg is sent after debounce delay to trigger validation
type validationTickMsg struct {
	branchName string // The branch to validate
}

// validationResultMsg is sent when validation completes
type validationResultMsg struct {
	branchName string               // The branch that was validated
	result     *SelectionValidation // The validation result
}

// Update handles message updates for the bubbletea model
func (m *TreeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Handle search mode input
		if m.inSearchMode {
			switch msg.String() {
			case KeyEsc:
				// Exit search mode - invalidates cache because search affects display
				m.inSearchMode = false
				m.searchQuery = ""
				m.updateSearchMatches()
				m.invalidateTreeCache()
				m.renderTree()
			case "backspace":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
					m.updateSearchMatches()
					m.invalidateTreeCache()
					m.renderTree()
					m.moveToFirstMatch()
				}
			case KeyEnter:
				// Exit search mode on enter (but don't select)
				m.inSearchMode = false
				m.renderTree()
			default:
				// Handle regular character input - invalidates cache because search affects display
				if msg.Key().Text != "" {
					m.searchQuery += msg.Key().Text
					m.updateSearchMatches()
					m.invalidateTreeCache()
					m.renderTree()
					m.moveToFirstMatch()
				}
			}
			return m, tea.Batch(cmds...)
		}

		// Normal mode key handling - use shared keys with vim support
		switch {
		case m.mode == TreeModeView && key.Matches(msg, m.treeKeys.Quit):
			m.canceled = true
			return m, tea.Quit
		case m.mode == TreeModeSelect && key.Matches(msg, m.selectKeys.Cancel):
			m.canceled = true
			return m, tea.Quit
		case m.mode == TreeModeSelect && key.Matches(msg, m.selectKeys.Search):
			// Enter search mode (only in select mode)
			m.inSearchMode = true
			m.searchQuery = ""
			m.updateSearchMatches()
			m.invalidateTreeCache() // Search affects display
			m.renderTree()
		case key.Matches(msg, m.treeKeys.Up):
			if len(m.branches) > 0 {
				newIndex := m.selectedIndex
				// Try to find the next selectable branch going up
				for attempts := 0; attempts < len(m.branches); attempts++ {
					if newIndex > 0 {
						newIndex--
					} else {
						newIndex = len(m.branches) - 1 // Wrap to last
					}
					// Stop if this branch is selectable
					if !m.nonSelectable[m.branches[newIndex].Name] {
						break
					}
				}
				m.selectedIndex = newIndex
				m.selectedBranch = m.branches[m.selectedIndex].Name
				m.renderTree() // Re-render with new selection (includes cursor and highlight)
				m.ensureVisible()
				// Schedule validation with debounce
				if cmd := m.scheduleValidation(m.selectedBranch); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		case key.Matches(msg, m.treeKeys.Down):
			if len(m.branches) > 0 {
				newIndex := m.selectedIndex
				// Try to find the next selectable branch going down
				for attempts := 0; attempts < len(m.branches); attempts++ {
					if newIndex < len(m.branches)-1 {
						newIndex++
					} else {
						newIndex = 0 // Wrap to first
					}
					// Stop if this branch is selectable
					if !m.nonSelectable[m.branches[newIndex].Name] {
						break
					}
				}
				m.selectedIndex = newIndex
				m.selectedBranch = m.branches[m.selectedIndex].Name
				m.renderTree() // Re-render with new selection (includes cursor and highlight)
				m.ensureVisible()
				// Schedule validation with debounce
				if cmd := m.scheduleValidation(m.selectedBranch); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		case key.Matches(msg, m.selectKeys.Select):
			if m.mode == TreeModeSelect {
				return m, tea.Quit
			}
			if m.selectedBranch != "" {
				m.collapsed[m.selectedBranch] = !m.collapsed[m.selectedBranch]
				m.invalidateTreeCache() // Collapse/expand changes tree structure
				m.renderTree()
			}
		case key.Matches(msg, m.selectKeys.Expand):
			if m.selectedBranch != "" {
				m.collapsed[m.selectedBranch] = !m.collapsed[m.selectedBranch]
				m.invalidateTreeCache() // Collapse/expand changes tree structure
				m.renderTree()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		firstRender := !m.ready
		if !m.ready {
			m.viewport = viewport.New(viewport.WithWidth(msg.Width), viewport.WithHeight(msg.Height-2))
			m.ready = true
			m.updateSearchMatches()
			m.invalidateTreeCache() // First render needs full tree
		} else {
			m.viewport.SetWidth(msg.Width)
			m.viewport.SetHeight(msg.Height - 2)
			// Note: width changes might affect rendering, but for simplicity
			// we don't invalidate cache here - lines are pre-built and width
			// mainly affects viewport scrolling
		}
		m.renderTree()

		// Find selectedIndex on first render
		if firstRender && m.selectedBranch != "" {
			for i, b := range m.branches {
				if b.Name == m.selectedBranch {
					m.selectedIndex = i
					break
				}
			}
			// Trigger initial validation for the starting selection
			if cmd := m.scheduleValidation(m.selectedBranch); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case enrichDataMsg:
		// Slow path: update with full enriched data
		m.log("enrichDataMsg received, ready=%v, renderer=%v", m.ready, m.renderer != nil)
		if m.renderer != nil {
			m.renderer.SetAnnotations(msg.annotations)
			m.invalidateTreeCache() // New data requires full re-render
			m.renderTree()
			m.log("Tree re-rendered with enriched data")
		}

	case PanicError:
		// A background command panicked - log it and continue gracefully
		// The error was already logged by SafeCmdFunc, but we note it here too
		m.log("Recovered from panic in %s: %v", msg.Source, msg.Err)

	case validationTickMsg:
		// Debounce timer fired - run validation if this branch is still selected
		if msg.branchName == m.selectedBranch && m.validateSelection != nil {
			cmds = append(cmds, m.runValidation(msg.branchName))
		} else {
			// Selection changed during debounce, clear pending state
			m.validationPending = false
		}

	case validationResultMsg:
		// Validation completed - update state if the branch is still selected
		m.validationPending = false
		if msg.branchName == m.selectedBranch {
			m.validationResult = msg.result
			m.lastValidatedBranch = msg.branchName
		}
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// renderTree updates the cached branches and viewport content with the current tree state.
// In inline mode or before the viewport is ready, View() uses the cached branches directly.
// Uses two-phase rendering for fast navigation: if only selection changed, reuses cached tree data.
func (m *TreeModel) renderTree() {
	if m.renderer == nil {
		return
	}

	mode := tree.RenderModeFull
	if m.mode == TreeModeSelect {
		mode = tree.RenderModeSelect
	}
	opts := tree.RenderOptions{
		Mode:           mode,
		SelectedBranch: m.selectedBranch,
		Collapsed:      m.collapsed,
		SearchQuery:    m.searchQuery,
		SearchMatches:  m.searchMatches,
		NonSelectable:  m.nonSelectable,
	}

	// Two-phase rendering: reuse cached tree data if valid, only update selection
	if m.cachedTreeValid && m.cachedTreeData != nil {
		// Fast path: apply selection to cached data
		m.branches = m.cachedTreeData.ApplySelection(m.selectedBranch)
	} else {
		// Slow path: full render and cache
		m.cachedTreeData = m.renderer.RenderStackCached(m.trunkName, opts)
		m.cachedTreeValid = true
		m.branches = m.cachedTreeData.ApplySelection(m.selectedBranch)
	}

	// Flatten lines for viewport/direct rendering
	totalLines := 0
	for _, b := range m.branches {
		totalLines += len(b.Lines)
	}
	m.cachedLines = make([]string, 0, totalLines)
	m.branchLineStart = make([]int, len(m.branches))
	lineOffset := 0
	for i, b := range m.branches {
		m.branchLineStart[i] = lineOffset
		m.cachedLines = append(m.cachedLines, b.Lines...)
		lineOffset += len(b.Lines)
	}

	// Update viewport with rendered content (skip in inline mode - viewport not used)
	if m.ready && !m.inline {
		m.viewport.SetContent(strings.Join(m.cachedLines, "\n"))
	}
}

func (m *TreeModel) ensureVisible() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.branches) {
		return
	}
	if m.selectedIndex >= len(m.branchLineStart) {
		return
	}

	lineOffset := m.branchLineStart[m.selectedIndex]

	branchHeight := len(m.branches[m.selectedIndex].Lines)

	// Simple viewport scrolling to keep selected branch visible
	if lineOffset < m.viewport.YOffset() {
		m.viewport.SetYOffset(lineOffset)
	} else if lineOffset+branchHeight > m.viewport.YOffset()+m.viewport.Height() {
		m.viewport.SetYOffset(lineOffset + branchHeight - m.viewport.Height())
	}
}

// updateSearchMatches updates the searchMatches map based on current searchQuery
func (m *TreeModel) updateSearchMatches() {
	m.searchMatches = make(map[string]bool)

	if m.searchQuery == "" {
		// All branches match when search is empty
		for _, b := range m.allBranches {
			m.searchMatches[b.GetName()] = true
		}
		return
	}

	query := strings.ToLower(m.searchQuery)
	for _, b := range m.allBranches {
		branchName := strings.ToLower(b.GetName())
		m.searchMatches[b.GetName()] = strings.Contains(branchName, query)
	}
}

// moveToFirstMatch moves selection to the first matching branch
func (m *TreeModel) moveToFirstMatch() {
	if m.searchQuery == "" {
		return
	}

	for i, b := range m.branches {
		if m.searchMatches[b.Name] {
			m.selectedIndex = i
			m.selectedBranch = b.Name
			m.ensureVisible()
			return
		}
	}
}

// scheduleValidation returns a command that triggers validation after debounce delay
func (m *TreeModel) scheduleValidation(branchName string) tea.Cmd {
	if m.validateSelection == nil {
		return nil
	}

	// Mark validation as pending
	m.validationPending = true

	// Return a tick command that will fire after debounce delay
	return tea.Tick(validationDebounceTime, func(_ time.Time) tea.Msg {
		return validationTickMsg{branchName: branchName}
	})
}

// runValidation runs the validation callback in a goroutine and returns a command
func (m *TreeModel) runValidation(branchName string) tea.Cmd {
	if m.validateSelection == nil {
		return nil
	}

	validateFn := m.validateSelection
	logger := m.logger

	return func() tea.Msg {
		defer func() {
			if p := recover(); p != nil && logger != nil {
				logger.Error("Validation panicked: %v", p)
			}
		}()

		result := validateFn(branchName)
		return validationResultMsg{
			branchName: branchName,
			result:     result,
		}
	}
}

// View renders the bubbletea model
func (m *TreeModel) View() tea.View {
	if m.renderer == nil {
		return tea.NewView("")
	}

	title := "Stackit Tree"
	help := "'q' quit, 'enter' expand/collapse, '↑/k' '↓/j' navigate"
	if m.mode == TreeModeSelect {
		if m.header != "" {
			title = m.header
		} else {
			title = "Select Branch"
		}
		if m.inSearchMode {
			help = fmt.Sprintf("Search: /%s (esc to exit, enter to confirm)", m.searchQuery)
		} else {
			help = "'/' search, 'esc' cancel, 'enter' select, 'space' expand, '↑/k' '↓/j' navigate"
		}
	}

	header := style.ColorDim(fmt.Sprintf(" %s | %d branches | %s", title, len(m.allBranches), help))

	// Render content - use viewport for full-screen mode, direct rendering for inline
	var content string
	switch {
	case m.ready && !m.inline:
		content = m.viewport.View()
	case len(m.cachedLines) > 0:
		// Use cached lines (already rendered by renderTree)
		content = strings.Join(m.cachedLines, "\n")
	default:
		// Fallback: render tree directly for immediate display before first renderTree call
		mode := tree.RenderModeFull
		if m.mode == TreeModeSelect {
			mode = tree.RenderModeSelect
		}
		opts := tree.RenderOptions{
			Mode:           mode,
			SelectedBranch: m.selectedBranch,
			Collapsed:      m.collapsed,
			SearchQuery:    m.searchQuery,
			SearchMatches:  m.searchMatches,
			NonSelectable:  m.nonSelectable,
		}
		branches := m.renderer.RenderStackDetailed(m.trunkName, opts)
		var lines []string
		for _, b := range branches {
			lines = append(lines, b.Lines...)
		}
		content = strings.Join(lines, "\n")
	}

	// Build the output
	parts := []string{header, "", content}

	// Add validation status footer if validation is enabled
	if m.validateSelection != nil && m.mode == TreeModeSelect {
		footer := m.renderValidationFooter()
		if footer != "" {
			parts = append(parts, "", footer)
		}
	}

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, parts...))
	if m.altScreen {
		v.AltScreen = true
	}
	return v
}

// renderValidationFooter renders the validation status footer
func (m *TreeModel) renderValidationFooter() string {
	if m.validationPending {
		return style.ColorDim(" ⏳ Checking for conflicts...")
	}

	if m.validationResult != nil && m.lastValidatedBranch == m.selectedBranch {
		if m.validationResult.Valid {
			return " " + style.ColorGreen("✓") + " " + style.ColorGreen(m.validationResult.Message)
		}
		return " " + style.ColorRed("✗") + " " + style.ColorRed(m.validationResult.Message)
	}

	return ""
}

// PromptTreeSelect runs the interactive log in selection mode and returns the selected branch name
func PromptTreeSelect(ctx context.Context, eng engine.Engine, ghClient github.Client, opts TreeOptions) (string, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return "", err
	}

	m := newTreeSelectModel(ctx, eng, ghClient, opts)

	m.altScreen = !opts.Inline

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	res := finalModel.(*TreeModel)
	if res.canceled {
		return "", errors.ErrCanceled
	}

	return res.selectedBranch, nil
}

// indexedBranch pairs a branch with its position so parallel workers can write
// results into a positional slice without coordinating on a shared map.
type indexedBranch struct {
	index  int
	branch engine.Branch
}

func indexedBranches(branches []engine.Branch) []indexedBranch {
	out := make([]indexedBranch, len(branches))
	for i, b := range branches {
		out[i] = indexedBranch{index: i, branch: b}
	}
	return out
}

// SelectionValidation contains the result of validating a selection
type SelectionValidation struct {
	Valid   bool   // Whether the selection is valid (no conflicts)
	Message string // Status message to display (e.g., "Move will complete without conflicts")
}

// TreeOptions repeated here to avoid circular dependency if needed,
// but we'll probably use actions.TreeOptions
type TreeOptions struct {
	Style               string
	ShowUntracked       bool
	Exclude             map[string]bool                  // Branches to exclude from selection
	NonSelectable       map[string]bool                  // Branches visible but not selectable (cursor skips them)
	AnnotationOverrides map[string]tree.BranchAnnotation // Override annotations (e.g., add custom labels)
	Logger              output.Logger                    // Optional logger for IO timing diagnostics
	Header              string                           // Custom header text for selection mode (e.g., "Select new parent for branch X")
	SkipEnrichment      bool                             // Skip background GitHub/git enrichment for faster startup
	Inline              bool                             // Run inline without alt-screen (doesn't take over terminal)

	// ValidateSelection is called (with debounce) when selection changes to validate the current selection.
	// If provided, the result is displayed in the UI footer.
	ValidateSelection func(branchName string) *SelectionValidation
}
