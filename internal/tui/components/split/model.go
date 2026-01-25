package split

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/tui/core"
	"stackit.dev/stackit/internal/tui/style"
)

// State represents the main state of the split wizard
type State int

// State constants
const (
	StateSelectingType State = iota
	StateSelectingDirection
	StateHunkLoop
	StateComplete
	StateCanceled
	StateError
)

// SubState represents the sub-state within the hunk loop
type SubState int

// SubState constants
const (
	SubStateNone SubState = iota
	SubStateSelectingHunks
	SubStateEnteringBranchName
	SubStatePromptEditMessage
	SubStateEditingMessage
	SubStateCreatingBranch
	SubStateWaitingForRetry
)

// Model is the unified TUI model for the split wizard.
// It implements a state machine that guides the user through:
// 1. Selecting split type (if not preselected)
// 2. Selecting direction (if not preselected)
// 3. The hunk selection loop (select hunks → branch name → commit message → create)
type Model struct {
	core.BaseModel

	// Configuration
	config Config

	// Current state
	state    State
	subState SubState

	// UI components
	help         help.Model
	branchInput  textinput.Model
	hunkSelector *tui.HunkSelectorModel
	keys         KeyMap
	styles       Styles

	// Type selection state
	availableTypes []TypeChoice
	typeCursor     int

	// Direction selection state
	direction Direction
	stackPath []string // Path from trunk to current branch

	// Hunk loop state
	currentHunks      []git.Hunk
	currentDiff       string
	createdBranches   []string
	sessionNames      []string
	commitMessage     string
	wantsEditMessage  bool
	defaultBranchName string

	// External state (for branch validation)
	existingBranchNames map[string]bool
	originalBranchName  string

	// Error handling
	errorMessage string

	// Result
	result Result
}

// NewModel creates a new split wizard model
func NewModel(cfg Config) *Model {
	ti := textinput.New()
	ti.Placeholder = "branch-name"
	ti.CharLimit = 100
	ti.Width = 40

	m := &Model{
		config:      cfg,
		help:        help.New(),
		branchInput: ti,
		keys:        DefaultKeyMap,
		styles:      DefaultStyles(),
		direction:   DirectionBelow, // Default
	}

	// Determine initial state based on preselected values
	if cfg.PreselectedStyle != "" {
		m.result.Style = cfg.PreselectedStyle
		if cfg.PreselectedDirection != "" {
			m.result.Direction = cfg.PreselectedDirection
			m.direction = cfg.PreselectedDirection
			m.state = StateHunkLoop
			m.subState = SubStateSelectingHunks
		} else {
			m.state = StateSelectingDirection
		}
	} else {
		m.state = StateSelectingType
		m.availableTypes = cfg.AvailableTypes
	}

	// Build stack path for direction visualization
	if cfg.Engine != nil && cfg.Branch.GetName() != "" {
		m.buildStackPath()
	}

	return m
}

// Init implements tea.Model
func (m *Model) Init() tea.Cmd {
	m.SignalReady()
	return m.InitSpinner()
}

// Update implements tea.Model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle common messages first
	if handled, cmd := m.HandleCommonMsg(msg); handled {
		// Don't quit on ctrl+c/q, instead mark as canceled
		if m.Done {
			m.state = StateCanceled
			m.result.Canceled = true
			return m, tea.Quit
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// BaseModel already updated Width/Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	// Handle custom messages
	case TypeSelectedMsg:
		m.result.Style = msg.Style
		m.state = StateSelectingDirection
		return m, nil

	case DirectionSelectedMsg:
		m.result.Direction = msg.Direction
		m.direction = msg.Direction
		m.state = StateHunkLoop
		m.subState = SubStateSelectingHunks
		return m, nil

	case HunksLoadedMsg:
		m.currentHunks = msg.Hunks
		m.currentDiff = msg.Diff
		if m.hunkSelector == nil {
			m.hunkSelector = tui.NewHunkSelectorModel(msg.Hunks)
		} else {
			m.hunkSelector.SetHunks(msg.Hunks)
		}
		return m, nil

	case HunksSelectedMsg:
		m.currentHunks = msg.Hunks
		m.subState = SubStateEnteringBranchName
		m.branchInput.SetValue(m.defaultBranchName)
		m.branchInput.Focus()
		return m, textinput.Blink

	case BranchNameEnteredMsg:
		m.sessionNames = append(m.sessionNames, msg.Name)
		m.subState = SubStatePromptEditMessage
		return m, nil

	case EditMessageConfirmedMsg:
		m.wantsEditMessage = msg.WantsEdit
		if msg.WantsEdit {
			m.subState = SubStateEditingMessage
			// Return a command that signals we need an external editor
			return m, func() tea.Msg { return EditorRequestMsg{DefaultMessage: m.commitMessage} }
		}
		m.subState = SubStateCreatingBranch
		return m, nil

	case EditorCompleteMsg:
		if msg.Error != nil {
			m.errorMessage = msg.Error.Error()
			m.state = StateError
			m.result.Error = msg.Error
			return m, tea.Quit
		}
		m.commitMessage = msg.Message
		m.subState = SubStateCreatingBranch
		return m, nil

	case BranchCreatedMsg:
		m.createdBranches = append(m.createdBranches, msg.Name)
		// Continue the loop - wait for more hunks
		m.subState = SubStateSelectingHunks
		return m, nil

	case NoChangesMsg:
		m.subState = SubStateWaitingForRetry
		return m, nil

	case RetryMsg:
		m.subState = SubStateSelectingHunks
		return m, nil

	case CompleteMsg:
		m.result.Branches = msg.Branches
		m.state = StateComplete
		return m, tea.Quit

	case CancelMsg:
		m.state = StateCanceled
		m.result.Canceled = true
		return m, tea.Quit

	case ErrorMsg:
		m.state = StateError
		m.result.Error = msg.Error
		m.errorMessage = msg.Error.Error()
		return m, tea.Quit
	}

	return m, nil
}

// handleKeyMsg handles keyboard input based on current state
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case StateSelectingType:
		return m.updateSelectingType(msg)
	case StateSelectingDirection:
		return m.updateSelectingDirection(msg)
	case StateHunkLoop:
		return m.updateHunkLoop(msg)
	}
	return m, nil
}

// updateSelectingType handles input during type selection
func (m *Model) updateSelectingType(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.typeCursor = max(0, m.typeCursor-1)
		// Skip unavailable options
		for m.typeCursor > 0 && !m.availableTypes[m.typeCursor].Available {
			m.typeCursor--
		}
	case key.Matches(msg, m.keys.Down):
		m.typeCursor = min(len(m.availableTypes)-1, m.typeCursor+1)
		// Skip unavailable options
		for m.typeCursor < len(m.availableTypes)-1 && !m.availableTypes[m.typeCursor].Available {
			m.typeCursor++
		}
	case key.Matches(msg, m.keys.Select):
		if m.typeCursor < len(m.availableTypes) && m.availableTypes[m.typeCursor].Available {
			m.result.Style = m.availableTypes[m.typeCursor].Style
			m.state = StateSelectingDirection
		}
	case key.Matches(msg, m.keys.Cancel):
		m.state = StateCanceled
		m.result.Canceled = true
		return m, tea.Quit
	}
	return m, nil
}

// updateSelectingDirection handles input during direction selection
func (m *Model) updateSelectingDirection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.direction = DirectionAbove
	case key.Matches(msg, m.keys.Down):
		m.direction = DirectionBelow
	case key.Matches(msg, m.keys.Select):
		m.result.Direction = m.direction
		m.state = StateHunkLoop
		m.subState = SubStateSelectingHunks
	case key.Matches(msg, m.keys.Cancel):
		m.state = StateCanceled
		m.result.Canceled = true
		return m, tea.Quit
	}
	return m, nil
}

// updateHunkLoop handles input during the hunk loop phase
func (m *Model) updateHunkLoop(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.subState {
	case SubStateEnteringBranchName:
		return m.updateBranchNameInput(msg)
	case SubStatePromptEditMessage:
		return m.updateEditMessagePrompt(msg)
	case SubStateWaitingForRetry:
		return m.updateRetryPrompt(msg)
	}
	return m, nil
}

// updateBranchNameInput handles branch name text input
func (m *Model) updateBranchNameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Submit):
		name := m.branchInput.Value()
		if name == "" {
			name = m.defaultBranchName
		}
		// Validate the name
		if err := m.validateBranchName(name); err != nil {
			m.errorMessage = err.Error()
			return m, nil
		}
		m.errorMessage = ""
		m.sessionNames = append(m.sessionNames, name)
		m.subState = SubStatePromptEditMessage
		return m, nil
	case key.Matches(msg, m.keys.Cancel):
		m.state = StateCanceled
		m.result.Canceled = true
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.branchInput, cmd = m.branchInput.Update(msg)
		return m, cmd
	}
}

// updateEditMessagePrompt handles the edit message yes/no prompt
func (m *Model) updateEditMessagePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Yes):
		m.wantsEditMessage = true
		m.subState = SubStateEditingMessage
		return m, func() tea.Msg { return EditorRequestMsg{DefaultMessage: m.commitMessage} }
	case key.Matches(msg, m.keys.No):
		m.wantsEditMessage = false
		m.subState = SubStateCreatingBranch
		return m, nil
	case key.Matches(msg, m.keys.Cancel):
		m.state = StateCanceled
		m.result.Canceled = true
		return m, tea.Quit
	}
	return m, nil
}

// updateRetryPrompt handles the retry yes/no prompt when no changes were staged
func (m *Model) updateRetryPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Yes):
		m.subState = SubStateSelectingHunks
		return m, nil
	case key.Matches(msg, m.keys.No):
		m.state = StateCanceled
		m.result.Canceled = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Cancel):
		m.state = StateCanceled
		m.result.Canceled = true
		return m, tea.Quit
	}
	return m, nil
}

// validateBranchName validates a branch name
func (m *Model) validateBranchName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	// Check if name is already used in this session
	for _, existing := range m.sessionNames {
		if existing == name {
			return fmt.Errorf("branch name %q is already used in this split", name)
		}
	}
	// Check if name exists in repo (except original branch)
	if name != m.originalBranchName && m.existingBranchNames[name] {
		return fmt.Errorf("branch name %q already exists in the repository", name)
	}
	return nil
}

// View implements tea.Model
func (m *Model) View() string {
	if m.Done || m.state == StateComplete || m.state == StateCanceled {
		return ""
	}

	switch m.state {
	case StateSelectingType:
		return m.viewSelectingType()
	case StateSelectingDirection:
		return m.viewSelectingDirection()
	case StateHunkLoop:
		return m.viewHunkLoop()
	case StateError:
		return m.viewError()
	}

	return "Loading..."
}

// viewSelectingType renders the type selection screen
func (m *Model) viewSelectingType() string {
	var sb strings.Builder

	sb.WriteString(m.styles.Header.Title.Render("How would you like to split this branch?"))
	sb.WriteString("\n\n")

	for i, choice := range m.availableTypes {
		cursor := "  "
		if i == m.typeCursor {
			cursor = m.styles.Selection.Cursor.Render("> ")
		}

		label := choice.Label
		desc := m.styles.Common.Dim.Render(" - " + choice.Description)

		if !choice.Available {
			label = m.styles.Selection.Unselected.Render(label)
			desc = m.styles.Selection.Unselected.Render(" (not available)")
		} else if i == m.typeCursor {
			label = m.styles.Selection.Cursor.Render(label)
		}

		sb.WriteString(cursor + label + desc + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(m.help.View(navigationKeyMap{m.keys}))

	return m.styles.Layout.Container.Render(sb.String())
}

// viewSelectingDirection renders the direction selection screen
func (m *Model) viewSelectingDirection() string {
	var sb strings.Builder

	sb.WriteString(m.styles.Header.Title.Render("Where should the new branch be placed?"))
	sb.WriteString("\n\n")

	// Direction options
	sb.WriteString(m.renderDirectionOptions())
	sb.WriteString("\n")

	// Stack tree preview
	sb.WriteString(m.renderStackTree())
	sb.WriteString("\n\n")

	sb.WriteString(m.help.View(navigationKeyMap{m.keys}))

	return m.styles.Layout.Container.Render(sb.String())
}

// renderDirectionOptions renders the up/down direction selection
func (m *Model) renderDirectionOptions() string {
	var sb strings.Builder

	// Above option
	if m.direction == DirectionAbove {
		sb.WriteString(m.styles.Selection.Cursor.Render("▸ Above"))
		sb.WriteString(m.styles.Common.Dim.Render(" - Insert as child of current"))
	} else {
		sb.WriteString(m.styles.Selection.Unselected.Render("  Above - Insert as child of current"))
	}
	sb.WriteString("\n")

	// Below option
	if m.direction == DirectionBelow {
		sb.WriteString(m.styles.Selection.Cursor.Render("▸ Below"))
		sb.WriteString(m.styles.Common.Dim.Render(" - Insert between parent and current"))
	} else {
		sb.WriteString(m.styles.Selection.Unselected.Render("  Below - Insert between parent and current"))
	}
	sb.WriteString("\n")

	return sb.String()
}

// renderStackTree renders the stack tree with insertion point
func (m *Model) renderStackTree() string {
	if m.config.Engine == nil || len(m.stackPath) == 0 {
		return ""
	}

	virtualTree := m.buildVirtualTree()
	renderer := tree.NewRenderer(virtualTree)

	// Set annotation for current branch
	renderer.SetAnnotation(m.config.Branch.GetName(), tree.BranchAnnotation{
		CustomLabel: "← current",
	})

	// Set annotation for the new branch placeholder
	renderer.SetAnnotation(newBranchPlaceholder, tree.BranchAnnotation{
		CustomLabel: "← new",
	})

	// Mark children as re-parented when direction is "above"
	if m.direction == DirectionAbove {
		graph := engine.BuildStackGraph(m.config.Engine, engine.SortStrategySmart, nil)
		for _, child := range graph.Children(m.config.Branch) {
			renderer.SetAnnotation(child, tree.BranchAnnotation{
				CustomLabel: "(re-parented)",
			})
		}
	}

	opts := tree.RenderOptions{
		Mode:                tree.RenderModeFull,
		HideSummary:         true,
		SkipSelectionPrefix: true,
	}

	lines := renderer.RenderStack(virtualTree.Trunk(), opts)

	// Style the new branch line with green color
	insertStyle := style.InsertStyle()
	for i, line := range lines {
		if strings.Contains(line, newBranchPlaceholder) {
			lines[i] = strings.Replace(line, newBranchPlaceholder, insertStyle.Render(newBranchPlaceholder), 1)
		}
	}

	return strings.Join(lines, "\n")
}

// viewHunkLoop renders the hunk loop state
func (m *Model) viewHunkLoop() string {
	var sb strings.Builder

	// Header showing progress
	sb.WriteString(m.styles.Header.Title.Render(fmt.Sprintf("Split Branch - Branch %d", len(m.createdBranches)+1)))
	sb.WriteString("\n\n")

	switch m.subState {
	case SubStateSelectingHunks:
		sb.WriteString("Select hunks to stage for the next branch...\n")
		sb.WriteString(m.styles.Common.Subtle.Render("(Hunks will be shown in full-screen selector)"))

	case SubStateEnteringBranchName:
		sb.WriteString("Enter branch name:\n\n")
		sb.WriteString(m.branchInput.View())
		if m.errorMessage != "" {
			sb.WriteString("\n")
			sb.WriteString(m.styles.Status.Error.Render("Error: " + m.errorMessage))
		}
		sb.WriteString("\n\n")
		sb.WriteString(m.help.View(submitKeyMap{m.keys}))

	case SubStatePromptEditMessage:
		sb.WriteString("Edit commit message?\n\n")
		sb.WriteString(m.styles.Common.Subtle.Render("Press 'y' for yes, 'n' for no"))
		sb.WriteString("\n\n")
		sb.WriteString(m.help.View(confirmKeyMap{m.keys}))

	case SubStateEditingMessage:
		sb.WriteString("Opening editor...")

	case SubStateCreatingBranch:
		sb.WriteString(m.Spinner.View() + " ")
		sb.WriteString(m.styles.Status.Active.Render("Creating branch..."))

	case SubStateWaitingForRetry:
		sb.WriteString(m.styles.Status.Error.Render("No changes were staged."))
		sb.WriteString("\n\nWould you like to try again?\n")
		sb.WriteString(m.styles.Common.Subtle.Render("Press 'y' to retry, 'n' to cancel"))
		sb.WriteString("\n\n")
		sb.WriteString(m.help.View(confirmKeyMap{m.keys}))
	}

	// Show created branches so far
	if len(m.createdBranches) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(m.styles.Header.Subtitle.Render("Created branches:"))
		sb.WriteString("\n")
		for _, name := range m.createdBranches {
			sb.WriteString(m.styles.Status.Done.Render("  " + m.styles.Icons.Done + " " + name))
			sb.WriteString("\n")
		}
	}

	return m.styles.Layout.Container.Render(sb.String())
}

// viewError renders an error state
func (m *Model) viewError() string {
	var sb strings.Builder

	sb.WriteString(m.styles.Status.Error.Render("Error: " + m.errorMessage))
	sb.WriteString("\n\n")
	sb.WriteString("Press any key to exit...")

	return m.styles.Layout.Container.Render(sb.String())
}

// buildStackPath builds the path from trunk to current branch
func (m *Model) buildStackPath() {
	if m.config.Engine == nil {
		return
	}

	graph := engine.BuildStackGraph(m.config.Engine, engine.SortStrategySmart, nil)
	currentName := m.config.Branch.GetName()

	var path []string
	current := currentName
	for current != "" && current != graph.Trunk {
		path = append([]string{current}, path...)
		node := graph.Nodes[current]
		if node == nil {
			break
		}
		current = graph.Parent(node.Branch)
	}
	// Add trunk at the beginning
	path = append([]string{graph.Trunk}, path...)
	m.stackPath = path
}

// buildVirtualTree creates a virtual tree structure for direction preview
func (m *Model) buildVirtualTree() *virtualTree {
	trunkBranch := ""
	if len(m.stackPath) > 0 {
		trunkBranch = m.stackPath[0]
	}

	graph := engine.BuildStackGraph(m.config.Engine, engine.SortStrategySmart, nil)
	children := graph.Children(m.config.Branch)

	return &virtualTree{
		stackPath:     m.stackPath,
		currentBranch: m.config.Branch.GetName(),
		trunkBranch:   trunkBranch,
		children:      children,
		direction:     m.direction,
	}
}

// GetResult returns the result of the split wizard
func (m *Model) GetResult() Result {
	return m.result
}

// SetExistingBranchNames sets the map of existing branch names for validation
func (m *Model) SetExistingBranchNames(names map[string]bool) {
	m.existingBranchNames = names
}

// SetOriginalBranchName sets the original branch name (allowed to reuse)
func (m *Model) SetOriginalBranchName(name string) {
	m.originalBranchName = name
}

// SetDefaultBranchName sets the default branch name for the next branch
func (m *Model) SetDefaultBranchName(name string) {
	m.defaultBranchName = name
	m.branchInput.SetValue(name)
}

// SetCommitMessage sets the default commit message
func (m *Model) SetCommitMessage(msg string) {
	m.commitMessage = msg
}

// GetCurrentSubState returns the current sub-state
func (m *Model) GetCurrentSubState() SubState {
	return m.subState
}

// GetCurrentState returns the current state
func (m *Model) GetCurrentState() State {
	return m.state
}

// GetSelectedDirection returns the selected direction
func (m *Model) GetSelectedDirection() Direction {
	return m.direction
}

// GetSelectedStyle returns the selected style
func (m *Model) GetSelectedStyle() Style {
	return m.result.Style
}

// GetBranchName returns the current branch name input value
func (m *Model) GetBranchName() string {
	return m.branchInput.Value()
}

// GetWantsEditMessage returns whether user wants to edit the message
func (m *Model) GetWantsEditMessage() bool {
	return m.wantsEditMessage
}

// IsWaitingForEditor returns true if the model is waiting for external editor
func (m *Model) IsWaitingForEditor() bool {
	return m.subState == SubStateEditingMessage
}

// IsSelectingHunks returns true if the model is in hunk selection state
func (m *Model) IsSelectingHunks() bool {
	return m.state == StateHunkLoop && m.subState == SubStateSelectingHunks
}

// IsCreatingBranch returns true if the model is creating a branch
func (m *Model) IsCreatingBranch() bool {
	return m.subState == SubStateCreatingBranch
}

const newBranchPlaceholder = "[new branch]"

// virtualTree implements tree.Data for direction preview
type virtualTree struct {
	stackPath     []string
	currentBranch string
	trunkBranch   string
	children      []string
	direction     Direction
}

func (v *virtualTree) CurrentBranch() string {
	return v.currentBranch
}

func (v *virtualTree) Trunk() string {
	return v.trunkBranch
}

func (v *virtualTree) Children(branchName string) []string {
	parentOfCurrent := ""
	currentIdx := -1
	for i, b := range v.stackPath {
		if b == v.currentBranch {
			currentIdx = i
			if i > 0 {
				parentOfCurrent = v.stackPath[i-1]
			}
			break
		}
	}

	switch v.direction {
	case DirectionBelow:
		if branchName == parentOfCurrent {
			return []string{newBranchPlaceholder}
		}
		if branchName == newBranchPlaceholder {
			return []string{v.currentBranch}
		}
		if branchName == v.currentBranch {
			return v.children
		}

	case DirectionAbove:
		if branchName == v.currentBranch {
			return []string{newBranchPlaceholder}
		}
		if branchName == newBranchPlaceholder {
			return v.children
		}
	}

	for i, b := range v.stackPath {
		if b == branchName && i+1 < len(v.stackPath) {
			nextInPath := v.stackPath[i+1]
			if v.direction == DirectionBelow && i == currentIdx-1 {
				return []string{newBranchPlaceholder}
			}
			return []string{nextInPath}
		}
	}

	return nil
}

func (v *virtualTree) Parent(branchName string) string {
	parentOfCurrent := ""
	for i, b := range v.stackPath {
		if b == v.currentBranch && i > 0 {
			parentOfCurrent = v.stackPath[i-1]
			break
		}
	}

	switch v.direction {
	case DirectionBelow:
		if branchName == newBranchPlaceholder {
			return parentOfCurrent
		}
		if branchName == v.currentBranch {
			return newBranchPlaceholder
		}

	case DirectionAbove:
		if branchName == newBranchPlaceholder {
			return v.currentBranch
		}
		for _, child := range v.children {
			if branchName == child {
				return newBranchPlaceholder
			}
		}
	}

	for i, b := range v.stackPath {
		if b == branchName && i > 0 {
			return v.stackPath[i-1]
		}
	}

	return ""
}

func (v *virtualTree) IsTrunk(branchName string) bool {
	return branchName == v.trunkBranch
}

func (v *virtualTree) IsFixed(_ string) bool {
	return true
}
