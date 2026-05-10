package tui

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Direction represents where to place a new branch
type Direction string

// Direction constants for split operations.
const (
	DirectionBelow Direction = "below"
	DirectionAbove Direction = "above"
)

const newBranchPlaceholder = "[new branch]"

// virtualDirectionTree implements tree.Data to show a tree with a virtual new branch
// inserted at the correct position based on the selected direction.
type virtualDirectionTree struct {
	stackPath     []string  // Path from trunk to current branch
	currentBranch string    // The actual current branch
	trunkBranch   string    // The trunk branch name
	children      []string  // Children of the current branch
	direction     Direction // Where to insert the new branch
}

// CurrentBranch returns the actual current branch name.
func (v *virtualDirectionTree) CurrentBranch() string {
	return v.currentBranch
}

// Trunk returns the trunk branch name.
func (v *virtualDirectionTree) Trunk() string {
	return v.trunkBranch
}

// Children returns the children of a branch, modified based on direction.
func (v *virtualDirectionTree) Children(branchName string) []string {
	// Find the parent of current (one step before current in stack path)
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
		// Insert [new branch] between parent and current
		// parent's children = [new branch], [new branch]'s children = [current]
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
		// Insert [new branch] as child of current
		// current's children = [new branch], [new branch]'s children = original children
		if branchName == v.currentBranch {
			return []string{newBranchPlaceholder}
		}
		if branchName == newBranchPlaceholder {
			return v.children
		}
	}

	// For other branches in the stack path, return their normal child
	for i, b := range v.stackPath {
		if b == branchName && i+1 < len(v.stackPath) {
			nextInPath := v.stackPath[i+1]
			// For DirectionBelow, skip if this is the parent→current relationship (we replaced it)
			if v.direction == DirectionBelow && i == currentIdx-1 {
				return []string{newBranchPlaceholder}
			}
			return []string{nextInPath}
		}
	}

	return nil
}

// Parent returns the parent of a branch, modified based on direction.
func (v *virtualDirectionTree) Parent(branchName string) string {
	// Find parent of current
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
		// Re-parented children now have [new branch] as parent
		if slices.Contains(v.children, branchName) {
			return newBranchPlaceholder
		}
	}

	// For other branches, return normal parent from stack path
	for i, b := range v.stackPath {
		if b == branchName && i > 0 {
			return v.stackPath[i-1]
		}
	}

	return ""
}

// IsTrunk returns whether the branch is the trunk branch.
func (v *virtualDirectionTree) IsTrunk(branchName string) bool {
	return branchName == v.trunkBranch
}

// IsFixed returns true for all branches (no restack indicators needed).
func (v *virtualDirectionTree) IsFixed(_ string) bool {
	return true
}

// directionSelectKeyMap defines the keybindings for direction selection
type directionSelectKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Submit key.Binding
	Back   key.Binding
	Cancel key.Binding
}

func (k directionSelectKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Submit, k.Back, k.Cancel}
}

func (k directionSelectKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Submit, k.Back, k.Cancel}}
}

var defaultDirectionKeys = directionSelectKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "above"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "below"),
	),
	Submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Back: key.NewBinding(
		key.WithKeys("backspace", "left", "h"),
		key.WithHelp("←/backspace", "back"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c", "esc", "q"),
		key.WithHelp("esc", "cancel"),
	),
}

// DirectionSelectModel is a bubbletea model for selecting split direction
type DirectionSelectModel struct {
	currentBranch string
	parentBranch  string
	children      []string
	direction     Direction
	done          bool
	ready         bool
	back          bool
	err           error
	help          help.Model
	keys          directionSelectKeyMap

	// Stack path from trunk to current branch (in order)
	stackPath []string
}

// NewDirectionSelectModel creates a model for selecting split direction
func NewDirectionSelectModel(eng engine.BranchReader, currentBranch, parentBranch string, children []string) *DirectionSelectModel {
	// Build the path from trunk to current branch
	graph := eng.Graph(engine.SortStrategySmart)

	var stackPath []string
	current := currentBranch
	for current != "" && current != graph.TrunkName() {
		stackPath = append([]string{current}, stackPath...)
		node := graph.GetNode(current)
		if node == nil {
			break
		}
		current = graph.Parent(node.Branch)
	}
	// Add trunk at the beginning
	stackPath = append([]string{graph.TrunkName()}, stackPath...)

	return &DirectionSelectModel{
		currentBranch: currentBranch,
		parentBranch:  parentBranch,
		children:      children,
		direction:     DirectionAbove, // Default to above (extract to child branch)
		help:          help.New(),
		keys:          defaultDirectionKeys,
		stackPath:     stackPath,
	}
}

// Init implements tea.Model.
func (m *DirectionSelectModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *DirectionSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ready = true
		return m, nil
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			m.direction = DirectionAbove
		case key.Matches(msg, m.keys.Down):
			m.direction = DirectionBelow
		case key.Matches(msg, m.keys.Submit):
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Back):
			m.back = true
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Cancel):
			m.err = errors.ErrCanceled
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m *DirectionSelectModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}

	if !m.ready {
		return tea.NewView("Loading...")
	}

	var sb strings.Builder

	// Title
	headerStyles := style.DefaultHeaderStyles()
	sb.WriteString(headerStyles.Title.Render("Where should the new branch be placed?"))
	sb.WriteString("\n\n")

	// Direction options at the top
	sb.WriteString(m.renderOptions())
	sb.WriteString("\n")

	// Render the stack tree with insertion point
	sb.WriteString(m.renderStackTree())
	sb.WriteString("\n\n")

	// Help
	sb.WriteString(m.help.View(m.keys))

	return tea.NewView(style.DefaultLayoutStyles().Container.Render(sb.String()))
}

// renderOptions renders the direction selection options
func (m *DirectionSelectModel) renderOptions() string {
	var sb strings.Builder

	selectionStyles := style.DefaultSelectionStyles()
	normalStyle := style.SubtleStyle()

	// Above option (shown first - visually at top of tree)
	if m.direction == DirectionAbove {
		sb.WriteString(selectionStyles.Highlighted.Render("▸ Above"))
		sb.WriteString(normalStyle.Render(" - Insert as child of current"))
	} else {
		sb.WriteString(normalStyle.Render("  Above - Insert as child of current"))
	}
	sb.WriteString("\n")

	// Below option
	if m.direction == DirectionBelow {
		sb.WriteString(selectionStyles.Highlighted.Render("▸ Below"))
		sb.WriteString(normalStyle.Render(" - Insert between parent and current"))
	} else {
		sb.WriteString(normalStyle.Render("  Below - Insert between parent and current"))
	}
	sb.WriteString("\n")

	return sb.String()
}

// renderStackTree renders the current stack with insertion point indicator using the tree component.
func (m *DirectionSelectModel) renderStackTree() string {
	// Build virtual tree with the new branch placeholder inserted
	virtualTree := m.buildVirtualTree()
	renderer := tree.NewRenderer(virtualTree)

	// Set annotation for current branch
	renderer.SetAnnotation(m.currentBranch, tree.BranchAnnotation{
		CustomLabel: "← current",
	})

	// Set annotation for the new branch placeholder
	renderer.SetAnnotation(newBranchPlaceholder, tree.BranchAnnotation{
		CustomLabel: "← new",
	})

	// Mark children as re-parented when direction is "above"
	if m.direction == DirectionAbove {
		for _, child := range m.children {
			renderer.SetAnnotation(child, tree.BranchAnnotation{
				CustomLabel: "(re-parented)",
			})
		}
	}

	opts := tree.RenderOptions{
		Mode:                tree.RenderModeFull, // Full format with │ connectors
		HideSummary:         true,                // Don't show stats/PR info
		SkipSelectionPrefix: true,
	}

	lines := renderer.RenderStack(virtualTree.Trunk(), opts)

	// Style the new branch line with green color
	insertStyle := style.InsertStyle()
	for i, line := range lines {
		if strings.Contains(line, newBranchPlaceholder) {
			// Replace the placeholder with styled version
			lines[i] = strings.Replace(line, newBranchPlaceholder, insertStyle.Render(newBranchPlaceholder), 1)
		}
	}

	return strings.Join(lines, "\n")
}

// buildVirtualTree creates a tree data structure with the virtual new branch inserted.
func (m *DirectionSelectModel) buildVirtualTree() *virtualDirectionTree {
	trunkBranch := ""
	if len(m.stackPath) > 0 {
		trunkBranch = m.stackPath[0]
	}

	return &virtualDirectionTree{
		stackPath:     m.stackPath,
		currentBranch: m.currentBranch,
		trunkBranch:   trunkBranch,
		children:      m.children,
		direction:     m.direction,
	}
}

// Direction returns the selected direction
func (m *DirectionSelectModel) Direction() Direction {
	return m.direction
}

// Err returns any error that occurred
func (m *DirectionSelectModel) Err() error {
	return m.err
}

// Back returns true if the user pressed back
func (m *DirectionSelectModel) Back() bool {
	return m.back
}

// PromptDirectionSelect shows an interactive direction selector and returns the chosen direction
func PromptDirectionSelect(eng engine.BranchReader, currentBranch, parentBranch string, children []string) (Direction, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return "", err
	}

	m := NewDirectionSelectModel(eng, currentBranch, parentBranch, children)

	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	model, err := p.Run()
	if err != nil {
		return "", err
	}

	if finalModel, ok := model.(*DirectionSelectModel); ok {
		if finalModel.Back() {
			return "", errors.ErrBack
		}
		if finalModel.Err() != nil {
			return "", finalModel.Err()
		}
		return finalModel.Direction(), nil
	}

	return "", fmt.Errorf("unexpected model type")
}
