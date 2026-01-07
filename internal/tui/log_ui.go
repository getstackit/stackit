package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/tui/keys"
	"stackit.dev/stackit/internal/tui/style"
	"stackit.dev/stackit/internal/utils"
)

const (
	logStyleFull = "FULL"
)

// LogMode defines how the log is used
type LogMode int

const (
	// LogModeView is the default view mode for browsing the log
	LogModeView LogMode = iota
	// LogModeSelect is the selection mode for choosing a branch
	LogModeSelect
)

// LogModel is the bubbletea model for the interactive log
type LogModel struct {
	context      context.Context
	engine       engine.Engine
	githubClient github.Client
	renderer     *tree.StackTreeRenderer
	viewport     viewport.Model
	width        int
	height       int
	ready        bool
	logger       output.Logger

	// Keys
	logKeys    keys.LogKeyMap
	selectKeys keys.SelectKeyMap

	// State
	mode           LogMode
	branches       []tree.RenderedBranch // Visible branches with their lines
	selectedIndex  int
	selectedBranch string
	collapsed      map[string]bool
	canceled       bool

	// Search state
	searchQuery   string
	inSearchMode  bool
	searchMatches map[string]bool // Branch name -> whether it matches search

	// Options
	style         string
	reverse       bool
	showUntracked bool
	exclude       map[string]bool
}

// NewLogModel creates a new LogModel
func NewLogModel(ctx context.Context, eng engine.Engine, ghClient github.Client, opts LogOptions) *LogModel {
	logger := opts.Logger
	logDebug := func(msg string, args ...any) {
		if logger != nil {
			logger.Debug(msg, args...)
		}
	}

	initStart := time.Now()
	logDebug("NewLogModel started")

	// Build filter function
	var filter func(string) bool
	if len(opts.Exclude) > 0 {
		filter = func(name string) bool {
			return !opts.Exclude[name]
		}
	}

	// Create renderer synchronously for instant display
	start := time.Now()
	renderer := NewStackTreeRendererWithStrategy(eng, engine.SortStrategySmart, filter)
	logDebug("NewStackTreeRendererWithStrategy completed in %v", time.Since(start))

	// Build minimal annotations synchronously (includes worktree info, no git/network calls)
	start = time.Now()
	annotations := make(map[string]tree.BranchAnnotation)
	for _, b := range eng.AllBranches() {
		annotations[b.GetName()] = GetMinimalAnnotationWithWorktree(eng, b)
	}
	renderer.SetAnnotations(annotations)
	logDebug("Minimal annotations with worktree completed in %v", time.Since(start))

	// Set initial selection
	start = time.Now()
	selectedBranch := ""
	if current := eng.CurrentBranch(); current != nil {
		selectedBranch = current.GetName()
	} else {
		selectedBranch = eng.Trunk().GetName()
	}
	logDebug("Initial selection completed in %v", time.Since(start))

	// Initialize search matches (all branches match when no search query)
	searchMatches := make(map[string]bool)
	for _, b := range eng.AllBranches() {
		searchMatches[b.GetName()] = true
	}

	logDebug("NewLogModel completed in %v", time.Since(initStart))

	m := &LogModel{
		context:        ctx,
		engine:         eng,
		githubClient:   ghClient,
		logger:         opts.Logger,
		renderer:       renderer,
		selectedBranch: selectedBranch,
		logKeys:        keys.DefaultLog,
		selectKeys:     keys.DefaultSelect,
		style:          opts.Style,
		reverse:        opts.Reverse,
		showUntracked:  opts.ShowUntracked,
		exclude:        opts.Exclude,
		collapsed:      make(map[string]bool),
		searchMatches:  searchMatches,
		mode:           LogModeView,
	}

	return m
}

// newLogSelectModel creates a new LogModel in selection mode
func newLogSelectModel(ctx context.Context, eng engine.Engine, ghClient github.Client, opts LogOptions) *LogModel {
	m := NewLogModel(ctx, eng, ghClient, opts)
	m.mode = LogModeSelect
	return m
}

// Init initializes the bubbletea model
func (m *LogModel) Init() tea.Cmd {
	// Renderer is already created with minimal data in NewLogModel.
	// Only run enrichment in the background.
	return m.enrichData()
}

// log logs a message if logger is available
func (m *LogModel) log(msg string, args ...any) {
	if m.logger != nil {
		m.logger.Debug(msg, args...)
	}
}

// enrichData returns a command that fetches full annotation data in the background.
// This includes git operations and network calls (remote SHAs, CI status).
func (m *LogModel) enrichData() tea.Cmd {
	// Capture values needed by the goroutine to avoid races on struct fields
	ctx := m.context
	eng := m.engine
	ghClient := m.githubClient
	style := m.style
	logger := m.logger

	logDebug := func(msg string, args ...any) {
		if logger != nil {
			logger.Debug(msg, args...)
		}
	}

	return func() tea.Msg {
		enrichStart := time.Now()
		logDebug("TUI enrichment started")

		allBranches := eng.AllBranches()

		// Channels for parallel results (buffered so goroutines don't block)
		type ciResult struct {
			statuses map[string]*github.CheckStatus
			err      error
		}
		ciChan := make(chan ciResult, 1)
		remoteShasDone := make(chan struct{}, 1)

		// Run PopulateRemoteShas and BatchGetPRChecksStatus in parallel
		if style == logStyleFull {
			go func() {
				start := time.Now()
				if err := eng.PopulateRemoteShas(); err != nil {
					logDebug("PopulateRemoteShas failed: %v", err)
				}
				logDebug("PopulateRemoteShas completed in %v", time.Since(start))
				remoteShasDone <- struct{}{}
			}()
		} else {
			remoteShasDone <- struct{}{}
		}

		if style == logStyleFull && ghClient != nil {
			branchNames := make([]string, 0, len(allBranches))
			for _, b := range allBranches {
				if !b.IsTrunk() {
					branchNames = append(branchNames, b.GetName())
				}
			}
			if len(branchNames) > 0 {
				go func() {
					start := time.Now()
					statuses, err := ghClient.BatchGetPRChecksStatus(ctx, branchNames)
					if err != nil {
						logDebug("BatchGetPRChecksStatus failed: %v", err)
					}
					logDebug("BatchGetPRChecksStatus for %d branches completed in %v", len(branchNames), time.Since(start))
					ciChan <- ciResult{statuses: statuses, err: err}
				}()
			} else {
				ciChan <- ciResult{}
			}
		} else {
			ciChan <- ciResult{}
		}

		// Wait for both operations to complete
		<-remoteShasDone
		ciStatuses := (<-ciChan).statuses

		// Collect full annotations
		start := time.Now()
		annotations := make(map[string]tree.BranchAnnotation)
		utils.Run(allBranches, func(b engine.Branch) {
			ann := GetBranchAnnotation(eng, b)
			// Add CI status and review status if available
			if style == logStyleFull && !b.IsTrunk() && ciStatuses != nil {
				if status := ciStatuses[b.GetName()]; status != nil {
					ann.CheckStatus = tree.CheckStatusPassing
					if status.Pending {
						ann.CheckStatus = tree.CheckStatusPending
					} else if !status.Passing {
						ann.CheckStatus = tree.CheckStatusFailing
					}

					// Map review decision to display format
					switch status.ReviewDecision {
					case "APPROVED":
						ann.ReviewStatus = "Approved"
					case "CHANGES_REQUESTED":
						ann.ReviewStatus = "Changes Requested"
					}
				}
			}
			// Check if this branch is a stack root with a managed worktree
			stackRoot := eng.GetStackRootForBranch(b)
			if stackRoot == b.GetName() {
				if wtInfo, err := eng.GetWorktreeForStack(stackRoot); err == nil && wtInfo != nil {
					ann.WorktreePath = wtInfo.Path
				}
			}
			annotations[b.GetName()] = ann
		})
		logDebug("Collected full annotations for %d branches in %v", len(allBranches), time.Since(start))

		logDebug("TUI enrichment completed in %v", time.Since(enrichStart))

		return enrichDataMsg{annotations: annotations}
	}
}

// enrichDataMsg is sent when full annotation data (including git/network) is ready
type enrichDataMsg struct {
	annotations map[string]tree.BranchAnnotation
}

// Update handles message updates for the bubbletea model
func (m *LogModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle search mode input
		if m.inSearchMode {
			switch msg.String() {
			case KeyEsc:
				// Exit search mode
				m.inSearchMode = false
				m.searchQuery = ""
				m.updateSearchMatches()
				m.renderTree()
			case "backspace":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
					m.updateSearchMatches()
					m.renderTree()
					m.moveToFirstMatch()
				}
			case KeyEnter:
				// Exit search mode on enter (but don't select)
				m.inSearchMode = false
				m.renderTree()
			default:
				// Handle regular character input
				if len(msg.Runes) > 0 {
					m.searchQuery += string(msg.Runes)
					m.updateSearchMatches()
					m.renderTree()
					m.moveToFirstMatch()
				}
			}
			return m, tea.Batch(cmds...)
		}

		// Normal mode key handling - use shared keys with vim support
		switch {
		case m.mode == LogModeView && key.Matches(msg, m.logKeys.Quit):
			m.canceled = true
			return m, tea.Quit
		case m.mode == LogModeSelect && key.Matches(msg, m.selectKeys.Cancel):
			m.canceled = true
			return m, tea.Quit
		case m.mode == LogModeSelect && key.Matches(msg, m.selectKeys.Search):
			// Enter search mode (only in select mode)
			m.inSearchMode = true
			m.searchQuery = ""
			m.updateSearchMatches()
			m.renderTree()
		case key.Matches(msg, m.logKeys.Up):
			if len(m.branches) > 0 {
				if m.selectedIndex > 0 {
					m.selectedIndex--
				} else {
					m.selectedIndex = len(m.branches) - 1 // Wrap to last
				}
				m.selectedBranch = m.branches[m.selectedIndex].Name
				m.renderTree()
				m.ensureVisible()
			}
		case key.Matches(msg, m.logKeys.Down):
			if len(m.branches) > 0 {
				if m.selectedIndex < len(m.branches)-1 {
					m.selectedIndex++
				} else {
					m.selectedIndex = 0 // Wrap to first
				}
				m.selectedBranch = m.branches[m.selectedIndex].Name
				m.renderTree()
				m.ensureVisible()
			}
		case key.Matches(msg, m.selectKeys.Select):
			if m.mode == LogModeSelect {
				return m, tea.Quit
			}
			if m.selectedBranch != "" {
				m.collapsed[m.selectedBranch] = !m.collapsed[m.selectedBranch]
				m.renderTree()
			}
		case key.Matches(msg, m.selectKeys.Expand):
			if m.selectedBranch != "" {
				m.collapsed[m.selectedBranch] = !m.collapsed[m.selectedBranch]
				m.renderTree()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		firstRender := !m.ready
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-2)
			m.ready = true
			m.updateSearchMatches()
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 2
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
		}

	case enrichDataMsg:
		// Slow path: update with full enriched data
		m.log("enrichDataMsg received, ready=%v, renderer=%v", m.ready, m.renderer != nil)
		if m.renderer != nil {
			m.renderer.SetAnnotations(msg.annotations)
			m.renderTree()
			m.log("Tree re-rendered with enriched data")
		}
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// renderTree updates the viewport content with the current tree state.
// This requires the viewport to be ready (initialized via WindowSizeMsg).
// Note: View() has its own fallback rendering for the !ready case to ensure
// immediate visual feedback before the viewport is initialized.
func (m *LogModel) renderTree() {
	if m.renderer == nil || !m.ready {
		return
	}

	trunk := m.engine.Trunk().GetName()
	opts := tree.RenderOptions{
		Reverse:        m.reverse,
		SelectedBranch: m.selectedBranch,
		Collapsed:      m.collapsed,
		SingleLine:     m.mode == LogModeSelect,
		SearchQuery:    m.searchQuery,
		SearchMatches:  m.searchMatches,
	}
	m.branches = m.renderer.RenderStackDetailed(trunk, opts)

	var allLines []string
	for _, b := range m.branches {
		allLines = append(allLines, b.Lines...)
	}

	m.viewport.SetContent(strings.Join(allLines, "\n"))
}

func (m *LogModel) ensureVisible() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.branches) {
		return
	}

	// Calculate the line offset for the selected branch
	lineOffset := 0
	for i := 0; i < m.selectedIndex; i++ {
		lineOffset += len(m.branches[i].Lines)
	}

	branchHeight := len(m.branches[m.selectedIndex].Lines)

	// Simple viewport scrolling to keep selected branch visible
	if lineOffset < m.viewport.YOffset {
		m.viewport.YOffset = lineOffset
	} else if lineOffset+branchHeight > m.viewport.YOffset+m.viewport.Height {
		m.viewport.YOffset = lineOffset + branchHeight - m.viewport.Height
	}
}

// updateSearchMatches updates the searchMatches map based on current searchQuery
func (m *LogModel) updateSearchMatches() {
	m.searchMatches = make(map[string]bool)
	if m.searchQuery == "" {
		// All branches match when search is empty - populate from engine
		allBranches := m.engine.AllBranches()
		for _, b := range allBranches {
			m.searchMatches[b.GetName()] = true
		}
		return
	}

	query := strings.ToLower(m.searchQuery)
	allBranches := m.engine.AllBranches()
	for _, b := range allBranches {
		branchName := strings.ToLower(b.GetName())
		m.searchMatches[b.GetName()] = strings.Contains(branchName, query)
	}
}

// moveToFirstMatch moves selection to the first matching branch
func (m *LogModel) moveToFirstMatch() {
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

// View renders the bubbletea model
func (m *LogModel) View() string {
	if m.renderer == nil {
		return ""
	}

	title := "Stackit Log"
	help := "'q' quit, 'enter' expand/collapse, '↑/k' '↓/j' navigate"
	if m.mode == LogModeSelect {
		title = "Select Branch"
		if m.inSearchMode {
			help = fmt.Sprintf("Search: /%s (esc to exit, enter to confirm)", m.searchQuery)
		} else {
			help = "'/' search, 'esc' cancel, 'enter' select, 'space' expand, '↑/k' '↓/j' navigate"
		}
	}

	header := style.ColorDim(fmt.Sprintf(" %s | %d branches | %s", title, len(m.engine.AllBranches()), help))

	// If viewport not ready yet, render tree directly without scrolling
	var content string
	if m.ready {
		content = m.viewport.View()
	} else {
		// Render tree directly for immediate display
		trunk := m.engine.Trunk().GetName()
		opts := tree.RenderOptions{
			Reverse:        m.reverse,
			SelectedBranch: m.selectedBranch,
			Collapsed:      m.collapsed,
			SingleLine:     m.mode == LogModeSelect,
			SearchQuery:    m.searchQuery,
			SearchMatches:  m.searchMatches,
		}
		branches := m.renderer.RenderStackDetailed(trunk, opts)
		var lines []string
		for _, b := range branches {
			lines = append(lines, b.Lines...)
		}
		content = strings.Join(lines, "\n")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		content,
	)
}

// PromptLogSelect runs the interactive log in selection mode and returns the selected branch name
func PromptLogSelect(ctx context.Context, eng engine.Engine, ghClient github.Client, opts LogOptions) (string, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return "", err
	}

	m := newLogSelectModel(ctx, eng, ghClient, opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	res := finalModel.(*LogModel)
	if res.canceled {
		return "", errors.ErrCanceled
	}

	return res.selectedBranch, nil
}

// LogOptions repeated here to avoid circular dependency if needed,
// but we'll probably use actions.LogOptions
type LogOptions struct {
	Style         string
	Reverse       bool
	ShowUntracked bool
	Exclude       map[string]bool // Branches to exclude from selection
	Logger        output.Logger   // Optional logger for IO timing diagnostics
}
