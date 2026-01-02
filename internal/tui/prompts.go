package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/tui/style"
	"stackit.dev/stackit/internal/utils"
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

// ErrInteractiveDisabled is returned when interactive prompts are disabled via STACKIT_TEST_NO_INTERACTIVE
var ErrInteractiveDisabled = fmt.Errorf("interactive prompts are disabled (STACKIT_TEST_NO_INTERACTIVE is set)")

// checkInteractiveAllowed returns an error if interactive mode is disabled
func checkInteractiveAllowed() error {
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

	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Submit):
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Cancel):
			m.err = fmt.Errorf("canceled")
			m.done = true
			return m, tea.Quit
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m textInputModel) View() string {
	if m.done {
		return ""
	}
	styleObj := lipgloss.NewStyle().Margin(1, 0)
	return styleObj.Render(fmt.Sprintf("%s\n%s\n\n%s", m.prompt, m.textInput.View(), m.help.View(m.keys)))
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
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Submit):
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Cancel):
			m.err = fmt.Errorf("canceled")
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

func (m confirmModel) View() string {
	if m.done {
		return ""
	}
	styleObj := lipgloss.NewStyle().Margin(1, 0)
	yesNo := "[y/N]"
	if m.choice {
		yesNo = "[Y/n]"
	}
	return styleObj.Render(fmt.Sprintf("%s %s\n\n%s", m.prompt, yesNo, m.help.View(confirmKeyMap{m.keys})))
}

func newTextInputModel(prompt, defaultValue string) textInputModel {
	ti := textinput.New()
	ti.Placeholder = ""
	ti.SetValue(defaultValue)
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 80

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

// PromptTextInput prompts the user for text input
func PromptTextInput(prompt, defaultValue string) (string, error) {
	if err := checkInteractiveAllowed(); err != nil {
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
	if err := checkInteractiveAllowed(); err != nil {
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
	list     list.Model
	selected string
	err      error
}

func (m promptListModel) Init() tea.Cmd {
	return nil
}

func (m promptListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.err = fmt.Errorf("canceled")
			return m, tea.Quit
		}
		if msg.String() == "enter" {
			if i, ok := m.list.SelectedItem().(listItem); ok {
				m.selected = i.value
				return m, tea.Quit
			}
		}
	case tea.WindowSizeMsg:
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m promptListModel) View() string {
	return lipgloss.NewStyle().Margin(1, 2).Render(m.list.View())
}

// NewSelectModel creates a tea.Model for a selection prompt (used by stories/demo)
func NewSelectModel(title string, options []SelectOption, defaultIndex int) tea.Model {
	items := make([]list.Item, len(options))
	for i, opt := range options {
		items[i] = listItem{title: opt.Label, value: opt.Value}
	}

	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)

	l := list.New(items, d, 0, 0)
	l.Title = title
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

	if defaultIndex >= 0 && defaultIndex < len(options) {
		l.Select(defaultIndex)
	}

	return promptListModel{list: l}
}

// NewBranchSelectModel creates a tea.Model for a branch selection prompt (used by stories/demo)
func NewBranchSelectModel(message string, choices []BranchChoice, initialIndex int) tea.Model {
	items := make([]list.Item, len(choices))
	for i, choice := range choices {
		items[i] = listItem{title: choice.Display, value: choice.Value}
	}

	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)

	l := list.New(items, d, 0, 0)
	l.Title = message
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

	if initialIndex >= 0 && initialIndex < len(choices) {
		l.Select(initialIndex)
	}

	return promptListModel{list: l}
}

// PromptSelect prompts the user to select from a list of options
func PromptSelect(title string, options []SelectOption, defaultIndex int) (string, error) {
	if err := checkInteractiveAllowed(); err != nil {
		return "", err
	}

	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}

	items := make([]list.Item, len(options))
	for i, opt := range options {
		items[i] = listItem{title: opt.Label, value: opt.Value}
	}

	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)

	l := list.New(items, d, 0, 0)
	l.Title = title
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

	if defaultIndex >= 0 && defaultIndex < len(options) {
		l.Select(defaultIndex)
	}

	m := promptListModel{list: l}

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

// BranchChoice represents a branch option in a selection prompt
type BranchChoice struct {
	Display string // What to show (may include tree visualization)
	Value   string // Actual branch name
}

// PromptBranchSelection prompts the user to select a branch
func PromptBranchSelection(message string, choices []BranchChoice, initialIndex int) (string, error) {
	if err := checkInteractiveAllowed(); err != nil {
		return "", err
	}

	items := make([]list.Item, len(choices))
	for i, choice := range choices {
		items[i] = listItem{title: choice.Display, value: choice.Value}
	}

	// Use a custom delegate that doesn't add padding/styling that might break tree visualization
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)

	l := list.New(items, d, 0, 0)
	l.Title = message
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

	if initialIndex >= 0 && initialIndex < len(choices) {
		l.Select(initialIndex)
	}

	m := promptListModel{list: l}

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
		annotations[branch.GetName()] = GetBranchAnnotation(eng, branch)
	}
	renderer.SetAnnotations(annotations)

	// Calculate depth for each branch to create proper tree indentation
	branchDepth := make(map[string]int)
	branchDepth[trunk.GetName()] = 0

	// Build depth map by traversing from trunk
	var calculateDepth func(branchName string, depth int)
	calculateDepth = func(branchName string, depth int) {
		branch := eng.GetBranch(branchName)
		children := branch.GetChildren()
		for _, child := range children {
			branchDepth[child.GetName()] = depth + 1
			calculateDepth(child.GetName(), depth+1)
		}
	}
	calculateDepth(trunk.GetName(), 0)

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
		coloredBranchName := style.ColorBranchName(branch.GetName(), isCurrent)

		// Add annotation
		annotation := annotations[branch.GetName()]
		coloredBranchName += renderer.FormatAnnotationColored(annotation)

		// Add restack indicator if needed
		if !eng.IsUpToDate(branch) {
			coloredBranchName += " " + style.ColorNeedsRestack("(needs restack)")
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
