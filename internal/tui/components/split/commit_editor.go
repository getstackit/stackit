package split

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

// CommitEditorConfig configures the commit message editor
type CommitEditorConfig struct {
	// DefaultMessage is the initial commit message
	DefaultMessage string
	// Files being extracted (for context display)
	Files []string
	// Direction of the split (above/below)
	Direction Direction
	// CurrentBranch name for context
	CurrentBranch string
}

// editorFinishedMsg is sent when the external editor closes
type editorFinishedMsg struct {
	content string
	err     error
}

// commitEditorKeyMap defines the keybindings for the commit editor
type commitEditorKeyMap struct {
	Edit   key.Binding
	Submit key.Binding
	Cancel key.Binding
}

func (k commitEditorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Edit, k.Submit, k.Cancel}
}

func (k commitEditorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Edit, k.Submit, k.Cancel}}
}

var defaultCommitEditorKeys = commitEditorKeyMap{
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit commit message"),
	),
	Submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc", "q"),
		key.WithHelp("esc", "cancel"),
	),
}

// CommitEditorModel is a bubbletea model for editing commit messages
// It shows context and launches external EDITOR for editing
type CommitEditorModel struct {
	config   CommitEditorConfig
	message  string
	help     help.Model
	keys     commitEditorKeyMap
	width    int
	height   int
	ready    bool
	done     bool
	canceled bool
	editing  bool
	tempFile string
}

// NewCommitEditorModel creates a new commit message editor model
func NewCommitEditorModel(cfg CommitEditorConfig) *CommitEditorModel {
	h := help.New()
	h.Styles.ShortKey = style.HelpKeyStyle()
	h.Styles.ShortDesc = style.HelpDescStyle()
	h.Styles.ShortSeparator = style.HelpSeparatorStyle()

	return &CommitEditorModel{
		config:  cfg,
		message: cfg.DefaultMessage,
		help:    h,
		keys:    defaultCommitEditorKeys,
	}
}

// Init implements tea.Model
func (m *CommitEditorModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m *CommitEditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

	case editorFinishedMsg:
		m.editing = false
		if msg.err != nil {
			// Editor failed, keep the current message
			return m, nil
		}
		// Clean the message (remove comments, trailing whitespace)
		m.message = utils.CleanCommitMessage(msg.content)
		return m, nil

	case tea.KeyPressMsg:
		if m.editing {
			// Ignore key presses while editor is open
			return m, nil
		}

		switch {
		case key.Matches(msg, m.keys.Edit):
			m.editing = true
			return m, m.openEditor()
		case key.Matches(msg, m.keys.Submit):
			if strings.TrimSpace(m.message) == "" {
				// Don't allow empty messages
				return m, nil
			}
			m.done = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Cancel):
			m.canceled = true
			m.done = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// openEditor launches the external EDITOR with the current message
func (m *CommitEditorModel) openEditor() tea.Cmd {
	// Create a temp file with the current message
	tmpFile, err := os.CreateTemp("", "COMMIT_EDITMSG-*")
	if err != nil {
		return func() tea.Msg {
			return editorFinishedMsg{err: err}
		}
	}
	m.tempFile = tmpFile.Name()

	// Write the current message and git-style comments
	content := m.message + "\n\n" +
		"# Enter the commit message for your changes.\n" +
		"# Lines starting with '#' will be ignored.\n"
	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(m.tempFile)
		return func() tea.Msg {
			return editorFinishedMsg{err: err}
		}
	}
	_ = tmpFile.Close()

	// Get editor from environment
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vim"
	}

	// Create the command
	c := exec.Command(editor, m.tempFile) //nolint:gosec
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	tempFile := m.tempFile
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			_ = os.Remove(tempFile)
			return editorFinishedMsg{err: err}
		}

		// Read the result
		content, readErr := os.ReadFile(tempFile)
		_ = os.Remove(tempFile)
		if readErr != nil {
			return editorFinishedMsg{err: readErr}
		}

		return editorFinishedMsg{content: string(content)}
	})
}

// View implements tea.Model
func (m *CommitEditorModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}

	if !m.ready {
		return tea.NewView("Loading...")
	}

	if m.editing {
		return tea.NewView("Opening editor...")
	}

	var sb strings.Builder
	headerStyles := style.DefaultHeaderStyles()

	// Title
	sb.WriteString(headerStyles.Title.Render("Commit Message"))
	sb.WriteString("\n\n")

	// Context panel showing what's being split
	sb.WriteString(m.renderContext())
	sb.WriteString("\n")

	// Current message in a box
	sb.WriteString(m.renderMessage())
	sb.WriteString("\n\n")

	// Help
	sb.WriteString(m.help.View(m.keys))

	return tea.NewView(style.DefaultLayoutStyles().Container.Render(sb.String()))
}

// renderContext shows the split context (files, direction, etc.)
func (m *CommitEditorModel) renderContext() string {
	var lines []string
	subtleStyle := style.SubtleStyle()

	// Show direction
	directionLabel := "child"
	if m.config.Direction == DirectionBelow {
		directionLabel = "parent"
	}
	lines = append(lines, subtleStyle.Render(fmt.Sprintf("Creating %s branch of %s", directionLabel, m.config.CurrentBranch)))
	lines = append(lines, "") // blank line

	// Show files being extracted
	if len(m.config.Files) > 0 {
		lines = append(lines, subtleStyle.Render("Extracting files:"))
		maxFiles := 5
		for i, f := range m.config.Files {
			if i >= maxFiles {
				lines = append(lines, subtleStyle.Render(fmt.Sprintf("  ... and %d more", len(m.config.Files)-maxFiles)))
				break
			}
			lines = append(lines, subtleStyle.Render(fmt.Sprintf("  %s", f)))
		}
	}

	return strings.Join(lines, "\n")
}

// renderMessage shows the current commit message in a styled box
func (m *CommitEditorModel) renderMessage() string {
	// Calculate width, ensuring reasonable minimum
	width := 70
	if m.width > 10 {
		width = min(70, m.width-4)
	}

	messageStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Width(width)

	msg := m.message
	if msg == "" {
		msg = "(empty - press 'e' to edit)"
	}

	return messageStyle.Render(msg)
}

// Message returns the commit message entered by the user
func (m *CommitEditorModel) Message() string {
	return strings.TrimSpace(m.message)
}

// Canceled returns true if the user canceled the editor
func (m *CommitEditorModel) Canceled() bool {
	return m.canceled
}

// GenerateDefaultCommitMessage generates a sensible default commit message
// based on the original commit message and files being extracted.
//
// Format:
//
//	<original title> (split)
//
//	<original body>
//
//	 * file1
//	 * file2
func GenerateDefaultCommitMessage(files []string, originalCommitMessage string) string {
	var sb strings.Builder

	// Parse original commit message into title and body
	title, body := parseCommitMessage(originalCommitMessage)

	// Write title with (split) suffix
	if title != "" {
		sb.WriteString(title)
		sb.WriteString(" (split)")
	} else {
		sb.WriteString("Split changes")
	}

	// Write body if present
	if body != "" {
		sb.WriteString("\n\n")
		sb.WriteString(body)
	}

	// Write file list
	if len(files) > 0 {
		sb.WriteString("\n\n")
		for _, f := range files {
			sb.WriteString(" * ")
			sb.WriteString(f)
			sb.WriteString("\n")
		}
	}

	return strings.TrimSpace(sb.String())
}

// parseCommitMessage splits a commit message into title (first line) and body (rest).
func parseCommitMessage(msg string) (title, body string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}

	// Split on first newline
	parts := strings.SplitN(msg, "\n", 2)
	title = strings.TrimSpace(parts[0])

	if len(parts) > 1 {
		body = strings.TrimSpace(parts[1])
	}

	return title, body
}
