package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/style"
)

const (
	// maxVisibleBranches is the maximum number of branches shown at once
	maxVisibleBranches = 12
	// moveValidationDebounce is the delay before triggering validation
	moveValidationDebounce = 300 * time.Millisecond
)

// moveState represents the current state of the move operation
type moveState int

const (
	moveStateSelecting  moveState = iota // User is selecting the target parent
	moveStateConfirming                  // User is confirming the move preview
	moveStateCanceled                    // User canceled the operation
	moveStateConfirmed                   // User confirmed, ready to execute
)

// MoveResult contains the result of the interactive move selection
type MoveResult struct {
	SelectedParent string              // The branch selected as new parent
	RebaseSpecs    []engine.RebaseSpec // Precomputed rebase specs for the selection
	Canceled       bool                // Whether the user canceled
}

// MoveModelConfig contains configuration for the move model
type MoveModelConfig struct {
	SourceBranch string          // Branch being moved
	Descendants  []engine.Branch // Descendants that will be restacked
	OldParent    *engine.Branch  // Current parent branch
	OldParentRev string          // Current parent revision for rebase specs
	Validator    MoveValidator   // Validation function for conflict checking
}

// MoveValidator validates a potential move operation
type MoveValidator func(ontoBranch string) (*MoveValidation, error)

// MoveValidation contains the result of validating a move
type MoveValidation struct {
	Valid          bool                // Whether the move is valid (no conflicts)
	Message        string              // Status message
	Commits        []string            // Commits that will be moved
	HasConflicts   bool                // Whether conflicts were detected
	ConflictBranch string              // Branch with conflicts
	ConflictError  string              // Conflict error message
	RebaseSpecs    []engine.RebaseSpec // Precomputed rebase specs for this selection
}

// branchItem represents a selectable branch in the list
type branchItem struct {
	name          string
	display       string   // Rendered display with tree characters (may be multi-line)
	displayLines  []string // Pre-split display lines (cached to avoid repeated splits in View)
	cursorLineIdx int      // Which line within display should have the cursor
	isSource      bool     // Is this the branch being moved?
	selectable    bool     // Can this branch be selected as a target?
}

// MoveModel is the bubbletea model for the interactive move flow
type MoveModel struct {
	// Core state
	state        moveState
	sourceBranch string
	oldParent    string

	// Branch list
	branches     []branchItem
	cursor       int
	scrollOffset int

	// Validation state
	validation        *MoveValidation
	validationPending bool
	validationTag     int // For debouncing
	validator         MoveValidator
	rebaseSpecs       []engine.RebaseSpec

	// Keybindings
	keys moveKeyMap
}

// moveKeyMap defines keys for the move selector
type moveKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Select  key.Binding
	Cancel  key.Binding
	Confirm key.Binding
	Back    key.Binding
}

var defaultMoveKeys = moveKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc", "q"),
		key.WithHelp("esc/q", "cancel"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("y", "enter"),
		key.WithHelp("y/enter", "confirm"),
	),
	Back: key.NewBinding(
		key.WithKeys("b", "esc"),
		key.WithHelp("b/esc", "back"),
	),
}

// NewMoveModel creates a new MoveModel for interactive branch selection
func NewMoveModel(eng engine.Engine, config MoveModelConfig) *MoveModel {
	// Build non-selectable set from descendants
	nonSelectable := make(map[string]bool)
	for _, d := range config.Descendants {
		nonSelectable[d.GetName()] = true
	}

	// Determine old parent name
	oldParentName := eng.Trunk().GetName()
	if config.OldParent != nil {
		oldParentName = config.OldParent.GetName()
	}

	// Build the branch list using the tree renderer for proper formatting
	trunk := eng.Trunk().GetName()

	// Create tree renderer
	renderer := NewStackTreeRenderer(eng)

	// Add custom annotation for source branch
	annotations := make(map[string]tree.BranchAnnotation)
	for _, b := range eng.AllBranches() {
		ann := tree.BranchAnnotation{}
		if b.GetName() == config.SourceBranch {
			ann.CustomLabel = " ← moving this"
		}
		annotations[b.GetName()] = ann
	}
	renderer.SetAnnotations(annotations)

	// Render the tree (trunk at bottom to match st tree)
	// Use SkipSelectionPrefix so we can add our own cursor indicators in viewSelecting
	opts := tree.RenderOptions{
		Mode:                tree.RenderModeSelect,
		NonSelectable:       nonSelectable,
		SkipSelectionPrefix: true,
	}
	rendered := renderer.RenderStackDetailed(trunk, opts)

	// Convert to branch items
	branches := make([]branchItem, 0, len(rendered))
	for _, rb := range rendered {
		isSource := rb.Name == config.SourceBranch
		selectable := !nonSelectable[rb.Name] && !isSource

		// Join all lines for this branch (includes connector lines like ├──┴──┘)
		// The cursor will be placed on the line at CursorLineIndex
		display := strings.Join(rb.Lines, "\n")

		branches = append(branches, branchItem{
			name:          rb.Name,
			display:       display,
			displayLines:  rb.Lines, // Cache pre-split lines to avoid repeated splits in View
			cursorLineIdx: rb.CursorLineIndex,
			isSource:      isSource,
			selectable:    selectable,
		})
	}

	// Find the source branch and trunk positions
	sourceIndex := 0
	trunkIndex := len(branches) - 1 // trunk is usually last (at bottom of tree)
	for i, b := range branches {
		if b.isSource {
			sourceIndex = i
		}
		if b.name == trunk {
			trunkIndex = i
		}
	}

	// Position cursor on trunk (main) as it's the most common move target
	cursor := trunkIndex
	// If trunk isn't selectable, find nearest selectable branch
	if !branches[cursor].selectable {
		for i := cursor - 1; i >= 0; i-- {
			if branches[i].selectable {
				cursor = i
				break
			}
		}
	}

	// Calculate scroll offset to show both source and trunk if possible
	// Start from the end to ensure trunk is visible
	scrollOffset := 0
	if len(branches) > maxVisibleBranches {
		// Show the last maxVisibleBranches items (trunk at bottom)
		scrollOffset = min(
			// But also try to show source branch
			sourceIndex, len(branches)-maxVisibleBranches)
	}

	return &MoveModel{
		state:        moveStateSelecting,
		sourceBranch: config.SourceBranch,
		oldParent:    oldParentName,
		branches:     branches,
		cursor:       cursor,
		scrollOffset: scrollOffset,
		validator:    config.Validator,
		keys:         defaultMoveKeys,
	}
}

// Init initializes the model
func (m *MoveModel) Init() tea.Cmd {
	// Trigger initial validation
	return m.scheduleValidation()
}

// moveValidationTickMsg is sent after debounce delay
type moveValidationTickMsg struct {
	tag int
}

// moveValidationResultMsg is sent when validation completes
type moveValidationResultMsg struct {
	branchName string
	validation *MoveValidation
	err        error
}

// Update handles messages and updates state
func (m *MoveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case moveStateSelecting:
		return m.updateSelecting(msg)
	case moveStateConfirming:
		return m.updateConfirming(msg)
	case moveStateCanceled, moveStateConfirmed:
		return m, tea.Quit
	}
	return m, nil
}

// updateSelecting handles messages in the selection state
func (m *MoveModel) updateSelecting(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			m.moveCursor(-1)
			return m, m.scheduleValidation()

		case key.Matches(msg, m.keys.Down):
			m.moveCursor(1)
			return m, m.scheduleValidation()

		case key.Matches(msg, m.keys.Select):
			if m.cursor >= 0 && m.cursor < len(m.branches) && m.branches[m.cursor].selectable {
				m.state = moveStateConfirming
				m.validationPending = true
				return m, m.runFullValidation()
			}

		case key.Matches(msg, m.keys.Cancel):
			m.state = moveStateCanceled
			return m, tea.Quit
		}

	case moveValidationTickMsg:
		if msg.tag == m.validationTag {
			return m, m.runValidation()
		}

	case moveValidationResultMsg:
		m.validationPending = false
		if msg.err != nil {
			m.validation = &MoveValidation{
				Valid:   false,
				Message: fmt.Sprintf("Error: %v", msg.err),
			}
			m.rebaseSpecs = nil
		} else {
			m.validation = msg.validation
			if msg.validation != nil {
				m.rebaseSpecs = msg.validation.RebaseSpecs
			} else {
				m.rebaseSpecs = nil
			}
		}
	}

	return m, nil
}

// updateConfirming handles messages in the confirmation state
func (m *MoveModel) updateConfirming(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Confirm):
			if m.validation != nil && m.validation.Valid {
				m.state = moveStateConfirmed
				return m, tea.Quit
			}
		case key.Matches(msg, m.keys.Back):
			m.state = moveStateSelecting
			return m, nil
		case key.Matches(msg, m.keys.Cancel):
			m.state = moveStateCanceled
			return m, tea.Quit
		}

	case moveValidationResultMsg:
		m.validationPending = false
		if msg.err != nil {
			m.validation = &MoveValidation{
				Valid:   false,
				Message: fmt.Sprintf("Error: %v", msg.err),
			}
			m.rebaseSpecs = nil
		} else {
			m.validation = msg.validation
			if msg.validation != nil {
				m.rebaseSpecs = msg.validation.RebaseSpecs
			} else {
				m.rebaseSpecs = nil
			}
		}
	}

	return m, nil
}

// moveCursor moves the cursor, skipping non-selectable branches
func (m *MoveModel) moveCursor(delta int) {
	if len(m.branches) == 0 {
		return
	}

	newCursor := m.cursor
	for attempts := 0; attempts < len(m.branches); attempts++ {
		newCursor += delta
		if newCursor < 0 {
			newCursor = len(m.branches) - 1
		} else if newCursor >= len(m.branches) {
			newCursor = 0
		}
		if m.branches[newCursor].selectable {
			m.cursor = newCursor
			m.ensureVisible()
			return
		}
	}
}

// ensureVisible adjusts scroll offset to keep cursor visible
func (m *MoveModel) ensureVisible() {
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	} else if m.cursor >= m.scrollOffset+maxVisibleBranches {
		m.scrollOffset = m.cursor - maxVisibleBranches + 1
	}
}

// scheduleValidation returns a command that triggers validation after debounce
func (m *MoveModel) scheduleValidation() tea.Cmd {
	if m.validator == nil {
		return nil
	}
	m.validationTag++
	m.validationPending = true
	tag := m.validationTag
	return tea.Tick(moveValidationDebounce, func(_ time.Time) tea.Msg {
		return moveValidationTickMsg{tag: tag}
	})
}

// runValidation runs quick validation for the current selection
func (m *MoveModel) runValidation() tea.Cmd {
	if m.validator == nil || m.cursor < 0 || m.cursor >= len(m.branches) {
		return nil
	}

	branchName := m.branches[m.cursor].name
	validator := m.validator

	return func() tea.Msg {
		result, err := validator(branchName)
		return moveValidationResultMsg{
			branchName: branchName,
			validation: result,
			err:        err,
		}
	}
}

// runFullValidation runs validation for confirmation (same as runValidation but clearer intent)
func (m *MoveModel) runFullValidation() tea.Cmd {
	return m.runValidation()
}

// selectedBranch returns the currently selected branch name
func (m *MoveModel) selectedBranch() string {
	if m.cursor >= 0 && m.cursor < len(m.branches) {
		return m.branches[m.cursor].name
	}
	return ""
}

// View renders the current state
func (m *MoveModel) View() tea.View {
	switch m.state {
	case moveStateSelecting:
		return tea.NewView(m.viewSelecting())
	case moveStateConfirming:
		return tea.NewView(m.viewConfirming())
	default:
		return tea.NewView("")
	}
}

// viewSelecting renders the branch selection view
func (m *MoveModel) viewSelecting() string {
	var sb strings.Builder

	// Header
	headerStyles := style.DefaultHeaderStyles()
	sb.WriteString(headerStyles.Title.Render(fmt.Sprintf("Select new parent for '%s'", m.sourceBranch)))
	sb.WriteString("\n\n")

	// Branch list with scroll
	visibleEnd := min(m.scrollOffset+maxVisibleBranches, len(m.branches))

	// Show scroll indicator if needed
	if m.scrollOffset > 0 {
		sb.WriteString(style.ColorDim("  ↑ more branches above"))
		sb.WriteString("\n")
	}

	for i := m.scrollOffset; i < visibleEnd; i++ {
		branch := m.branches[i]

		for lineIdx, line := range branch.displayLines {
			// Cursor indicator - only on the cursorLineIdx for the selected branch
			if i == m.cursor && lineIdx == branch.cursorLineIdx {
				sb.WriteString(style.SelectionCursorStyle().Render(style.SelectionCursor))
			} else {
				sb.WriteString(style.SelectionPadding)
			}

			// Branch display
			if branch.selectable {
				sb.WriteString(line)
			} else {
				sb.WriteString(style.ColorDim(line))
			}
			sb.WriteString("\n")
		}
	}

	// Show scroll indicator if needed
	if visibleEnd < len(m.branches) {
		sb.WriteString(style.ColorDim("  ↓ more branches below"))
		sb.WriteString("\n")
	}

	// Validation status
	sb.WriteString("\n")
	sb.WriteString(m.renderValidationStatus())

	// Help
	sb.WriteString("\n")
	sb.WriteString(style.ColorDim("↑/k up • ↓/j down • enter select • esc cancel"))
	sb.WriteString("\n")

	return style.DefaultLayoutStyles().Container.Render(sb.String())
}

// renderValidationStatus renders the validation footer
func (m *MoveModel) renderValidationStatus() string {
	if m.validationPending {
		return style.ColorDim("⏳ Checking for conflicts...")
	}

	if m.validation != nil {
		if m.validation.Valid {
			return style.ColorGreen("✓") + " " + style.ColorGreen(m.validation.Message)
		}
		return style.ColorRed("✗") + " " + style.ColorRed(m.validation.Message)
	}

	return ""
}

// viewConfirming renders the confirmation view
func (m *MoveModel) viewConfirming() string {
	var sb strings.Builder

	headerStyles := style.DefaultHeaderStyles()
	sb.WriteString(headerStyles.Title.Render("Confirm Move"))
	sb.WriteString("\n\n")

	// Show what we're doing
	fmt.Fprintf(&sb, "Move %s onto %s\n",
		style.ColorBranchName(m.sourceBranch, true),
		style.ColorBranchName(m.selectedBranch(), false))
	sb.WriteString("\n")

	// Validation status
	if m.validationPending {
		sb.WriteString(style.ColorDim("⏳ Validating move..."))
		sb.WriteString("\n")
	} else if m.validation != nil {
		if m.validation.Valid {
			sb.WriteString(style.ColorGreen("✓ Move will complete without conflicts"))
			sb.WriteString("\n\n")
			sb.WriteString(style.ColorDim("[y/enter] confirm • [b/esc] back • [q] cancel"))
		} else {
			sb.WriteString(style.ColorRed("✗ ") + style.ColorRed(m.validation.Message))
			sb.WriteString("\n\n")
			sb.WriteString(style.ColorDim("[b/esc] back • [q] cancel"))
		}
		sb.WriteString("\n")
	}

	return style.DefaultLayoutStyles().Container.Render(sb.String())
}

// Result returns the result of the move selection
func (m *MoveModel) Result() MoveResult {
	return MoveResult{
		SelectedParent: m.selectedBranch(),
		RebaseSpecs:    m.rebaseSpecs,
		Canceled:       m.state == moveStateCanceled,
	}
}

// PromptMoveSelect runs the interactive move selection and returns the selection result.
func PromptMoveSelect(eng engine.Engine, config MoveModelConfig) (MoveResult, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return MoveResult{}, err
	}

	model := NewMoveModel(eng, config)
	// No tea.WithAltScreen() - run inline
	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		return MoveResult{}, err
	}

	result := finalModel.(*MoveModel).Result()
	if result.Canceled {
		return MoveResult{}, errors.ErrCanceled
	}

	return result, nil
}
