package tui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

type promptKeyMap struct {
	Submit key.Binding
	Cancel key.Binding
	Yes    key.Binding
	No     key.Binding
}

func (k promptKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Cancel}
}

func (k promptKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.Cancel},
	}
}

var defaultPromptKeys = promptKeyMap{
	Submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c", "esc"),
		key.WithHelp("ctrl+c/esc", "cancel"),
	),
}

var confirmPromptKeys = promptKeyMap{
	Submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm default"),
	),
	Yes: key.NewBinding(
		key.WithKeys("y", "Y"),
		key.WithHelp("y", "yes"),
	),
	No: key.NewBinding(
		key.WithKeys("n", "N"),
		key.WithHelp("n", "no"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c", "esc"),
		key.WithHelp("ctrl+c", "cancel"),
	),
}

type confirmKeyMap struct {
	promptKeyMap
}

func (k confirmKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Yes, k.No, k.Submit, k.Cancel}
}

func (k confirmKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Yes, k.No, k.Submit, k.Cancel},
	}
}

type selectionKeys struct {
	Up     key.Binding
	Down   key.Binding
	Submit key.Binding
	Cancel key.Binding
}

func (k selectionKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Submit, k.Cancel}
}

func (k selectionKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Submit, k.Cancel},
	}
}

var defaultSelectionKeys = selectionKeys{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c", "esc"),
		key.WithHelp("ctrl+c/esc", "cancel"),
	),
}

// ErrInteractiveDisabled is returned when interactive prompts are disabled
var ErrInteractiveDisabled = fmt.Errorf("interactive prompts are disabled (running with --no-interactive or not in a TTY)")

// CheckInteractiveAllowed returns an error if interactive mode is disabled
func CheckInteractiveAllowed() error {
	if !utils.IsInteractive() {
		return ErrInteractiveDisabled
	}
	return nil
}

// textInputModel is a simple text input prompt model
type textInputModel struct {
	textInput textinput.Model
	prompt    string
	done      bool
	err       error
	help      help.Model
	keys      promptKeyMap
}

func (m textInputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m textInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Submit):
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Cancel):
			m.err = errors.ErrCanceled
			m.done = true
			return m, tea.Quit
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m textInputModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	styleObj := lipgloss.NewStyle().Margin(1, 0)
	return tea.NewView(styleObj.Render(fmt.Sprintf("%s\n%s\n\n%s", m.prompt, m.textInput.View(), m.help.View(m.keys))))
}

// confirmModel is a simple yes/no confirmation prompt model
type confirmModel struct {
	prompt string
	choice bool
	done   bool
	err    error
	help   help.Model
	keys   promptKeyMap
}

func (m confirmModel) Init() tea.Cmd {
	return nil
}

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Submit):
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Cancel):
			m.err = errors.ErrCanceled
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Yes):
			m.choice = true
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.No):
			m.choice = false
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	styleObj := lipgloss.NewStyle().Margin(1, 0)
	yesNo := "[y/N]"
	if m.choice {
		yesNo = "[Y/n]"
	}
	return tea.NewView(styleObj.Render(fmt.Sprintf("%s %s\n\n%s", m.prompt, yesNo, m.help.View(confirmKeyMap{m.keys}))))
}

func newTextInputModel(prompt, defaultValue string) textInputModel {
	ti := textinput.New()
	ti.Placeholder = ""
	ti.SetValue(defaultValue)
	ti.Focus()
	ti.CharLimit = 500
	ti.SetWidth(80)

	return textInputModel{
		textInput: ti,
		prompt:    prompt,
		help:      help.New(),
		keys:      defaultPromptKeys,
	}
}

// NewTextInputModel creates a tea.Model for a text input prompt (used by stories/demo)
func NewTextInputModel(prompt, defaultValue string) tea.Model {
	return newTextInputModel(prompt, defaultValue)
}

// NewConfirmModel creates a tea.Model for a confirmation prompt (used by stories/demo)
func NewConfirmModel(prompt string, defaultValue bool) tea.Model {
	return confirmModel{
		prompt: prompt,
		choice: defaultValue,
		help:   help.New(),
		keys:   confirmPromptKeys,
	}
}

// PromptTextInput prompts the user for text input. It is a var so tests can
// stub it, mirroring PromptConfirm.
var PromptTextInput = func(prompt, defaultValue string) (string, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return "", err
	}

	m := newTextInputModel(prompt, defaultValue)

	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	model, err := p.Run()
	if err != nil {
		return "", err
	}

	if finalModel, ok := model.(textInputModel); ok {
		if finalModel.err != nil {
			return "", finalModel.err
		}
		return finalModel.textInput.Value(), nil
	}

	return "", fmt.Errorf("unexpected model type")
}

// PromptConfirm prompts the user for yes/no confirmation
var PromptConfirm = func(prompt string, defaultValue bool) (bool, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return false, err
	}

	m := confirmModel{
		prompt: prompt,
		choice: defaultValue,
		help:   help.New(),
		keys:   confirmPromptKeys,
	}

	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	model, err := p.Run()
	if err != nil {
		return false, err
	}

	if finalModel, ok := model.(confirmModel); ok {
		if finalModel.err != nil {
			return false, finalModel.err
		}
		return finalModel.choice, nil
	}

	return false, fmt.Errorf("unexpected model type")
}

// SelectOption represents an option in a selection prompt
type SelectOption struct {
	Label string // What to show
	Value string // Value to return
}

type listItem struct {
	title, desc string
	value       string
}

func (i listItem) Title() string       { return i.title }
func (i listItem) Description() string { return i.desc }
func (i listItem) FilterValue() string { return i.title }

// promptListModel wraps bubbles/list to work with our Prompt functions
type promptListModel struct {
	list      list.Model
	selected  string
	err       error
	itemCount int
}

func (m promptListModel) Init() tea.Cmd {
	return nil
}

func (m promptListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.err = errors.ErrCanceled
			return m, tea.Quit
		}
		if msg.String() == "enter" {
			if i, ok := m.list.SelectedItem().(listItem); ok {
				m.selected = i.value
				return m, tea.Quit
			}
		}
	case tea.WindowSizeMsg:
		h, _ := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		// Limit height to number of items + title + help (roughly 4 extra lines)
		maxHeight := min(m.itemCount+4, msg.Height)
		m.list.SetSize(msg.Width-h, maxHeight)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m promptListModel) View() tea.View {
	return tea.NewView(lipgloss.NewStyle().Margin(1, 2).Render(m.list.View()))
}

// selectionModel is a simple selection prompt model
type selectionModel struct {
	title   string
	options []SelectOption
	cursor  int
	done    bool
	err     error
	help    help.Model
}

func (m selectionModel) Init() tea.Cmd {
	return nil
}

func (m selectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, defaultSelectionKeys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, defaultSelectionKeys.Down):
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case key.Matches(msg, defaultSelectionKeys.Submit):
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, defaultSelectionKeys.Cancel):
			m.err = errors.ErrCanceled
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectionModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}

	headerStyles := style.DefaultHeaderStyles()
	selectionStyles := style.DefaultSelectionStyles()
	layoutStyles := style.DefaultLayoutStyles()

	var s strings.Builder
	s.WriteString(headerStyles.Title.Render(m.title) + "\n\n")

	for i, opt := range m.options {
		if m.cursor == i {
			s.WriteString(selectionStyles.Highlighted.Render("> "+opt.Label) + "\n")
		} else {
			s.WriteString("  " + opt.Label + "\n")
		}
	}

	s.WriteString("\n" + m.help.View(defaultSelectionKeys))

	return tea.NewView(layoutStyles.Container.Render(s.String()))
}

// NewSelectModel creates a tea.Model for a selection prompt (used by stories/demo)
func NewSelectModel(title string, options []SelectOption, defaultIndex int) tea.Model {
	return selectionModel{
		title:   title,
		options: options,
		cursor:  defaultIndex,
		help:    help.New(),
	}
}

// NewBranchSelectModel creates a tea.Model for a branch selection prompt (used by stories/demo)
func NewBranchSelectModel(message string, choices []BranchChoice, initialIndex int) tea.Model {
	items := make([]list.Item, len(choices))
	for i, choice := range choices {
		items[i] = listItem{title: choice.Display, value: choice.Value}
	}

	headerStyles := style.DefaultHeaderStyles()
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)
	// Remove border and use consistent padding with arrow indicator
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, false).
		Foreground(lipgloss.Color(style.ColorPrimary)).
		Bold(true).
		PaddingLeft(2)
	d.Styles.NormalTitle = lipgloss.NewStyle().
		PaddingLeft(4)

	l := list.New(items, d, 0, 0)
	l.Title = message
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = headerStyles.Title

	if initialIndex >= 0 && initialIndex < len(choices) {
		l.Select(initialIndex)
	}

	return promptListModel{list: l, itemCount: len(choices)}
}

// PromptSelect prompts the user to select from a list of options
func PromptSelect(title string, options []SelectOption, defaultIndex int) (string, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return "", err
	}

	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}

	m := selectionModel{
		title:   title,
		options: options,
		cursor:  defaultIndex,
		help:    help.New(),
	}

	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	model, err := p.Run()
	if err != nil {
		return "", err
	}

	if finalModel, ok := model.(selectionModel); ok {
		if finalModel.err != nil {
			return "", finalModel.err
		}
		if finalModel.cursor >= 0 && finalModel.cursor < len(options) {
			return options[finalModel.cursor].Value, nil
		}
	}

	return "", fmt.Errorf("unexpected model type or selection")
}

// multiSelectKeyMap defines keybindings for multi-select prompt
type multiSelectKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Toggle key.Binding
	All    key.Binding
	None   key.Binding
	Submit key.Binding
	Cancel key.Binding
}

func (k multiSelectKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.All, k.None, k.Submit, k.Cancel}
}

func (k multiSelectKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Toggle, k.All, k.None, k.Submit, k.Cancel}}
}

var defaultMultiSelectKeys = multiSelectKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Toggle: key.NewBinding(key.WithKeys("space", " "), key.WithHelp("space", "toggle")),
	All:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
	None:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "none")),
	Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
	Cancel: key.NewBinding(key.WithKeys("ctrl+c", "esc", "q"), key.WithHelp("esc", "cancel")),
}

// multiSelectModel is a multi-select prompt model
type multiSelectModel struct {
	title    string
	options  []string
	selected map[int]bool
	cursor   int
	done     bool
	err      error
	help     help.Model
	keys     multiSelectKeyMap
}

func (m multiSelectModel) Init() tea.Cmd {
	return nil
}

func (m multiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case key.Matches(msg, m.keys.Toggle):
			m.selected[m.cursor] = !m.selected[m.cursor]
		case key.Matches(msg, m.keys.All):
			for i := range m.options {
				m.selected[i] = true
			}
		case key.Matches(msg, m.keys.None):
			m.selected = make(map[int]bool)
		case key.Matches(msg, m.keys.Submit):
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

func (m multiSelectModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}

	headerStyles := style.DefaultHeaderStyles()
	selectionStyles := style.DefaultSelectionStyles()
	layoutStyles := style.DefaultLayoutStyles()

	var s strings.Builder
	s.WriteString(headerStyles.Title.Render(m.title) + "\n\n")

	for i, opt := range m.options {
		checkbox := "[ ]"
		if m.selected[i] {
			checkbox = selectionStyles.Selected.Render("[x]")
		}

		line := fmt.Sprintf("%s %s", checkbox, opt)
		if m.cursor == i {
			s.WriteString(selectionStyles.Highlighted.Render("> "+line) + "\n")
		} else {
			s.WriteString("  " + line + "\n")
		}
	}

	// Show selection count
	count := 0
	for _, v := range m.selected {
		if v {
			count++
		}
	}
	fmt.Fprintf(&s, "\n%d of %d selected\n", count, len(m.options))
	s.WriteString("\n" + m.help.View(m.keys))

	return tea.NewView(layoutStyles.Container.Render(s.String()))
}

// PromptMultiSelect prompts the user to select multiple items from a list
func PromptMultiSelect(title string, options []string) ([]string, error) {
	return PromptMultiSelectWithDefaults(title, options, nil)
}

// PromptMultiSelectWithDefaults prompts the user to select multiple items from a list
// with optional pre-selected items. If preSelected is nil, nothing is selected by default.
func PromptMultiSelectWithDefaults(title string, options []string, preSelected []bool) ([]string, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return nil, err
	}

	if len(options) == 0 {
		return nil, fmt.Errorf("no options provided")
	}

	selected := make(map[int]bool)
	for i, sel := range preSelected {
		if i < len(options) {
			selected[i] = sel
		}
	}

	m := multiSelectModel{
		title:    title,
		options:  options,
		selected: selected,
		cursor:   0,
		help:     help.New(),
		keys:     defaultMultiSelectKeys,
	}

	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	model, err := p.Run()
	if err != nil {
		return nil, err
	}

	if finalModel, ok := model.(multiSelectModel); ok {
		if finalModel.err != nil {
			return nil, finalModel.err
		}
		var result []string
		for i, opt := range options {
			if finalModel.selected[i] {
				result = append(result, opt)
			}
		}
		return result, nil
	}

	return nil, fmt.Errorf("unexpected model type")
}

// BranchChoice represents a branch option in a selection prompt
type BranchChoice struct {
	Display string // What to show (may include tree visualization)
	Value   string // Actual branch name
}

// PromptBranchSelection prompts the user to select a branch
func PromptBranchSelection(message string, choices []BranchChoice, initialIndex int) (string, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return "", err
	}

	items := make([]list.Item, len(choices))
	for i, choice := range choices {
		items[i] = listItem{title: choice.Display, value: choice.Value}
	}

	// Use a custom delegate that doesn't add padding/styling that might break tree visualization
	headerStyles := style.DefaultHeaderStyles()
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)
	// Remove border and use consistent padding with arrow indicator
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, false).
		Foreground(lipgloss.Color(style.ColorPrimary)).
		Bold(true).
		PaddingLeft(2)
	d.Styles.NormalTitle = lipgloss.NewStyle().
		PaddingLeft(4)

	l := list.New(items, d, 0, 0)
	l.Title = message
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = headerStyles.Title

	if initialIndex >= 0 && initialIndex < len(choices) {
		l.Select(initialIndex)
	}

	m := promptListModel{list: l, itemCount: len(choices)}

	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	model, err := p.Run()
	if err != nil {
		return "", err
	}

	if finalModel, ok := model.(promptListModel); ok {
		if finalModel.err != nil {
			return "", finalModel.err
		}
		return finalModel.selected, nil
	}

	return "", fmt.Errorf("unexpected model type")
}

// PromptBranchCheckout shows an interactive branch selector for checkout.
// It takes a list of branches and the engine context, formats them using tree rendering,
// and presents them for selection.
func PromptBranchCheckout(branches []engine.Branch, eng engine.BranchReader) (string, error) {
	if len(branches) == 0 {
		return "", fmt.Errorf("no branches available to checkout")
	}

	// Create tree renderer
	currentBranch := eng.CurrentBranch()
	trunk := eng.Trunk()
	renderer := NewStackTreeRenderer(eng)

	// Add annotations for all branches
	annotations := make(map[string]tree.BranchAnnotation)
	for _, branch := range branches {
		annotations[branch.GetName()] = GetBranchAnnotation(eng, branch, AnnotationOptions{})
	}
	renderer.SetAnnotations(annotations)

	// Build StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Calculate depth for each branch to create proper tree indentation
	branchDepth := make(map[string]int)
	branchDepth[trunk.GetName()] = 0

	// Build depth map by traversing from trunk
	var calculateDepth func(branch engine.Branch, depth int)
	calculateDepth = func(branch engine.Branch, depth int) {
		children := graph.Children(branch)
		for _, childName := range children {
			branchDepth[childName] = depth + 1
			calculateDepth(eng.GetBranch(childName), depth+1)
		}
	}
	calculateDepth(trunk, 0)

	choices := make([]BranchChoice, 0, len(branches))
	initialIndex := -1

	for i, branch := range branches {
		isCurrent := currentBranch != nil && branch.GetName() == currentBranch.GetName()
		if isCurrent {
			initialIndex = i
		}

		// Get depth for indentation
		depth := branchDepth[branch.GetName()]

		// Create tree line with proper indentation
		indent := strings.Repeat("  ", depth)
		var symbol string
		if isCurrent {
			symbol = tree.CurrentBranchSymbol
		} else {
			symbol = tree.BranchSymbol
		}

		// Get colored branch name
		coloredBranchName := style.ColorBranchNameWithTrunk(branch.GetName(), isCurrent, branch.IsTrunk())

		// Add annotation
		annotation := annotations[branch.GetName()]
		coloredBranchName += renderer.FormatAnnotationColored(annotation)

		// Add restack indicator if needed
		if !eng.IsUpToDate(branch) {
			coloredBranchName += " " + style.ColorNeedsRestack(tree.RestackSuggestedLabel)
		}

		display := indent + symbol + " " + coloredBranchName

		choices = append(choices, BranchChoice{
			Display: display,
			Value:   branch.GetName(),
		})
	}

	// Set initial index if not found
	if initialIndex < 0 {
		initialIndex = len(choices) - 1
	}

	// Show interactive selector
	selected, err := PromptBranchSelection("Checkout a branch (arrow keys to navigate, type to filter)", choices, initialIndex)
	if err != nil {
		return "", err
	}

	return selected, nil
}
