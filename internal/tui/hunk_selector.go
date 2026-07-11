package tui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/tui/style"
)

// defaultPreviewLines is the number of diff lines to show in the collapsed preview
const defaultPreviewLines = 4

// HunkItem represents a single hunk in the selector
type HunkItem struct {
	Hunk       git.Hunk
	Selected   bool
	Expanded   bool // Manual expand/collapse override
	Splittable bool // Can this hunk be split further?
}

// FileGroup groups hunks by file for rendering
type FileGroup struct {
	File   string
	Hunks  []int // Indices into the items slice
	Binary bool  // Is this a binary file?
}

// hunkSelectorKeyMap defines the keybindings for hunk selection
type hunkSelectorKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Toggle   key.Binding
	Split    key.Binding
	All      key.Binding
	None     key.Binding
	Expand   key.Binding
	FileAll  key.Binding
	Submit   key.Binding
	Cancel   key.Binding
	PageUp   key.Binding
	PageDown key.Binding
}

func (k hunkSelectorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.Split, k.All, k.None, k.Submit, k.Cancel}
}

func (k hunkSelectorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown},
		{k.Toggle, k.Split, k.All, k.None},
		{k.Expand, k.FileAll},
		{k.Submit, k.Cancel},
	}
}

var defaultHunkSelectorKeys = hunkSelectorKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Toggle: key.NewBinding(
		key.WithKeys("space", " "),
		key.WithHelp("space", "toggle"),
	),
	Split: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "split"),
	),
	All: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "all"),
	),
	None: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "none"),
	),
	Expand: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "expand"),
	),
	FileAll: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "file toggle"),
	),
	Submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "done"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c", "esc", "q"),
		key.WithHelp("esc", "cancel"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup", "ctrl+u"),
		key.WithHelp("pgup", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown", "ctrl+d"),
		key.WithHelp("pgdn", "page down"),
	),
}

// HunkSelectorModel is a bubbletea model for selecting hunks.
// It embeds core.BaseModel for standard lifecycle handling.
type HunkSelectorModel struct {
	core.BaseModel // Embedded for ReadySignaler interface

	items      []HunkItem
	fileGroups []FileGroup
	cursor     int
	viewport   viewport.Model
	help       help.Model
	keys       hunkSelectorKeyMap
	err        error
	ready      bool // viewport ready flag (separate from BaseModel)

	// Preview settings
	previewLines int // Number of lines to show in collapsed preview
}

// NewHunkSelectorModel creates a new hunk selector model
func NewHunkSelectorModel(hunks []git.Hunk) *HunkSelectorModel {
	items := make([]HunkItem, len(hunks))
	fileMap := make(map[string][]int)
	fileOrder := make([]string, 0)
	fileBinary := make(map[string]bool)

	for i, h := range hunks {
		items[i] = HunkItem{
			Hunk:       h,
			Selected:   false,
			Expanded:   false,
			Splittable: !h.Binary && h.CanSplit(),
		}
		if _, exists := fileMap[h.File]; !exists {
			fileOrder = append(fileOrder, h.File)
		}
		fileMap[h.File] = append(fileMap[h.File], i)
		if h.Binary {
			fileBinary[h.File] = true
		}
	}

	fileGroups := make([]FileGroup, len(fileOrder))
	for i, file := range fileOrder {
		fileGroups[i] = FileGroup{
			File:   file,
			Hunks:  fileMap[file],
			Binary: fileBinary[file],
		}
	}

	return &HunkSelectorModel{
		items:        items,
		fileGroups:   fileGroups,
		cursor:       0,
		help:         help.New(),
		keys:         defaultHunkSelectorKeys,
		previewLines: defaultPreviewLines,
	}
}

// Init implements tea.Model
func (m *HunkSelectorModel) Init() tea.Cmd {
	m.SignalReady()
	return nil
}

// Update implements tea.Model
func (m *HunkSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height

		headerHeight := 3 // Title + summary line + blank
		footerHeight := 4 // Help + padding
		m.viewport = viewport.New(viewport.WithWidth(msg.Width), viewport.WithHeight(msg.Height-headerHeight-footerHeight))
		m.ready = true
		m.updateViewportContent()
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Cancel):
			m.err = errors.ErrCanceled
			m.Done = true
			return m, tea.Quit

		case key.Matches(msg, m.keys.Submit):
			// Require at least one selection before allowing submit
			hasSelection := false
			for _, item := range m.items {
				if item.Selected {
					hasSelection = true
					break
				}
			}
			if !hasSelection {
				// Don't allow submit with zero selections - user should select at least one hunk
				// or press cancel/escape to abort
				return m, nil
			}
			m.Done = true
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.updateViewportContent()
				m.ensureCursorVisible()
			}

		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.items)-1 {
				m.cursor++
				m.updateViewportContent()
				m.ensureCursorVisible()
			}

		case key.Matches(msg, m.keys.Toggle):
			if m.cursor >= 0 && m.cursor < len(m.items) {
				m.items[m.cursor].Selected = !m.items[m.cursor].Selected
				m.updateViewportContent()
			}

		case key.Matches(msg, m.keys.Split):
			if m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].Splittable {
				m.splitCurrentHunk()
				m.updateViewportContent()
			}

		case key.Matches(msg, m.keys.All):
			for i := range m.items {
				m.items[i].Selected = true
			}
			m.updateViewportContent()

		case key.Matches(msg, m.keys.None):
			for i := range m.items {
				m.items[i].Selected = false
			}
			m.updateViewportContent()

		case key.Matches(msg, m.keys.Expand):
			if m.cursor >= 0 && m.cursor < len(m.items) {
				m.items[m.cursor].Expanded = !m.items[m.cursor].Expanded
				m.updateViewportContent()
			}

		case key.Matches(msg, m.keys.FileAll):
			m.toggleFileSelection()
			m.updateViewportContent()

		case key.Matches(msg, m.keys.PageUp):
			m.viewport.SetYOffset(m.viewport.YOffset() - m.viewport.Height()/2)
			return m, nil

		case key.Matches(msg, m.keys.PageDown):
			m.viewport.SetYOffset(m.viewport.YOffset() + m.viewport.Height()/2)
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View implements tea.Model
func (m *HunkSelectorModel) View() tea.View {
	if m.Done {
		return tea.NewView("")
	}

	if !m.ready {
		return tea.NewView("Initializing...")
	}

	var sb strings.Builder

	// Header
	headerStyles := style.DefaultHeaderStyles()
	sb.WriteString(headerStyles.Title.Render(m.renderHeader()))
	sb.WriteString("\n\n")

	// Viewport with hunks
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")

	// Help
	sb.WriteString(m.help.View(m.keys))

	v := tea.NewView(style.DefaultLayoutStyles().Container.Render(sb.String()))
	v.AltScreen = true
	return v
}

// renderHeader creates the header line with selection info
func (m *HunkSelectorModel) renderHeader() string {
	selected := 0
	totalAdded := 0
	totalRemoved := 0
	for _, item := range m.items {
		if item.Selected {
			selected++
			added, removed := item.Hunk.LineCounts()
			totalAdded += added
			totalRemoved += removed
		}
	}

	fileCount := len(m.fileGroups)
	header := fmt.Sprintf("Select hunks to stage (%d of %d hunks, %d files)", selected, len(m.items), fileCount)
	if selected > 0 {
		header += fmt.Sprintf(" [+%d -%d lines]", totalAdded, totalRemoved)
	} else {
		header += " - select at least one to continue"
	}
	return header
}

// updateViewportContent renders all hunks into the viewport
func (m *HunkSelectorModel) updateViewportContent() {
	if !m.ready {
		return
	}

	var sb strings.Builder

	// Styles
	selectionStyles := style.DefaultSelectionStyles()
	headerStyles := style.DefaultHeaderStyles()
	fileStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(style.ColorSecondary))
	selectedStyle := selectionStyles.Selected
	unselectedStyle := selectionStyles.Unselected
	cursorStyle := headerStyles.Title
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(style.ColorSuccessAlt))
	removeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(style.ColorErrorAlt))
	contextStyle := style.DimStyle()
	splittableStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(style.ColorWarningAlt))
	binaryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(style.ColorMerged))

	for _, fg := range m.fileGroups {
		// File header
		fileHeader := fg.File
		if fg.Binary {
			fileHeader += " " + binaryStyle.Render("[binary]")
		}
		sb.WriteString(fileStyle.Render(fileHeader))
		sb.WriteString("\n")

		for _, hunkIdx := range fg.Hunks {
			item := m.items[hunkIdx]
			isCursor := hunkIdx == m.cursor

			// Checkbox and cursor
			var checkbox string
			if item.Selected {
				checkbox = selectedStyle.Render("[x]")
			} else {
				checkbox = unselectedStyle.Render("[ ]")
			}

			cursor := "  "
			if isCursor {
				cursor = cursorStyle.Render("> ")
			}

			// Handle binary files differently
			if item.Hunk.Binary {
				headerText := binaryStyle.Render("Binary file change")
				if isCursor {
					headerText = cursorStyle.Render("Binary file change")
				}
				fmt.Fprintf(&sb, "%s%s %s\n", cursor, checkbox, headerText)
				continue
			}

			// Hunk header line
			header := item.Hunk.Header()
			added, removed := item.Hunk.LineCounts()

			headerText := fmt.Sprintf("%s +%d -%d", header, added, removed)
			if item.Splittable {
				headerText += " " + splittableStyle.Render("[splittable]")
			}

			if isCursor {
				headerText = cursorStyle.Render(headerText)
			}

			fmt.Fprintf(&sb, "%s%s %s\n", cursor, checkbox, headerText)

			// Show preview for current hunk or expanded hunks
			if isCursor || item.Expanded {
				preview, totalLines, hasMore := item.Hunk.Preview(m.previewLines)
				if item.Expanded {
					// Show full content
					preview, _, _ = item.Hunk.Preview(totalLines)
				}

				// Render diff lines with colors
				previewLines := strings.Split(preview, "\n")
				maxPreview := m.previewLines
				if item.Expanded {
					maxPreview = len(previewLines)
				}

				for i, line := range previewLines {
					if i >= maxPreview && !item.Expanded {
						break
					}

					var renderedLine string
					switch {
					case strings.HasPrefix(line, "+"):
						renderedLine = addStyle.Render(line)
					case strings.HasPrefix(line, "-"):
						renderedLine = removeStyle.Render(line)
					default:
						renderedLine = contextStyle.Render(line)
					}
					sb.WriteString("      " + renderedLine + "\n")
				}

				if hasMore && !item.Expanded {
					remaining := totalLines - m.previewLines
					fmt.Fprintf(&sb, "      %s\n", contextStyle.Render(fmt.Sprintf("...%d more lines", remaining)))
				}
			}
		}
		sb.WriteString("\n")
	}

	m.viewport.SetContent(sb.String())
}

// ensureCursorVisible scrolls the viewport to keep cursor in view
func (m *HunkSelectorModel) ensureCursorVisible() {
	// Calculate approximate line position of cursor
	linePos := 0
	for _, fg := range m.fileGroups {
		linePos++ // File header
		for _, hunkIdx := range fg.Hunks {
			if hunkIdx == m.cursor {
				// Found cursor position
				if linePos < m.viewport.YOffset() {
					m.viewport.SetYOffset(linePos)
				} else if linePos > m.viewport.YOffset()+m.viewport.Height()-5 {
					m.viewport.SetYOffset(linePos - m.viewport.Height() + 5)
				}
				return
			}
			linePos++ // Hunk header line

			// Calculate actual preview size for this hunk
			item := m.items[hunkIdx]
			if hunkIdx == m.cursor || item.Expanded {
				// Binary files have no preview
				if item.Hunk.Binary {
					continue
				}
				_, totalLines, _ := item.Hunk.Preview(m.previewLines)
				if item.Expanded {
					linePos += totalLines
				} else {
					// Collapsed preview shows at most previewLines, plus "...N more lines" line if truncated
					previewSize := totalLines
					if previewSize > m.previewLines {
						previewSize = m.previewLines + 1 // +1 for the "...N more lines" line
					}
					linePos += previewSize
				}
			}
		}
		linePos++ // Blank line after file
	}
}

// splitCurrentHunk splits the current hunk if possible
func (m *HunkSelectorModel) splitCurrentHunk() {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return
	}

	item := m.items[m.cursor]
	if !item.Splittable {
		return
	}

	splitHunks, err := item.Hunk.Split()
	if err != nil || len(splitHunks) <= 1 {
		return
	}

	// Replace the current hunk with the split hunks
	newItems := make([]HunkItem, 0, len(m.items)+len(splitHunks)-1)
	newItems = append(newItems, m.items[:m.cursor]...)

	for _, h := range splitHunks {
		newItems = append(newItems, HunkItem{
			Hunk:       h,
			Selected:   item.Selected, // Preserve selection state
			Expanded:   false,
			Splittable: h.CanSplit(),
		})
	}

	newItems = append(newItems, m.items[m.cursor+1:]...)
	m.items = newItems

	// Rebuild file groups
	m.rebuildFileGroups()
}

// rebuildFileGroups rebuilds the file groups after a split
func (m *HunkSelectorModel) rebuildFileGroups() {
	fileMap := make(map[string][]int)
	fileOrder := make([]string, 0)
	fileBinary := make(map[string]bool)

	for i, item := range m.items {
		if _, exists := fileMap[item.Hunk.File]; !exists {
			fileOrder = append(fileOrder, item.Hunk.File)
		}
		fileMap[item.Hunk.File] = append(fileMap[item.Hunk.File], i)
		if item.Hunk.Binary {
			fileBinary[item.Hunk.File] = true
		}
	}

	m.fileGroups = make([]FileGroup, len(fileOrder))
	for i, file := range fileOrder {
		m.fileGroups[i] = FileGroup{
			File:   file,
			Hunks:  fileMap[file],
			Binary: fileBinary[file],
		}
	}
}

// toggleFileSelection toggles all hunks in the current file
func (m *HunkSelectorModel) toggleFileSelection() {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return
	}

	currentFile := m.items[m.cursor].Hunk.File

	// Find all hunks in this file and check if any are unselected
	anyUnselected := false
	for _, item := range m.items {
		if item.Hunk.File == currentFile && !item.Selected {
			anyUnselected = true
			break
		}
	}

	// Toggle: if any unselected, select all; otherwise deselect all
	for i := range m.items {
		if m.items[i].Hunk.File == currentFile {
			m.items[i].Selected = anyUnselected
		}
	}
}

// SelectedHunks returns the hunks that were selected
func (m *HunkSelectorModel) SelectedHunks() []git.Hunk {
	var selected []git.Hunk
	for _, item := range m.items {
		if item.Selected {
			selected = append(selected, item.Hunk)
		}
	}
	return selected
}

// SetHunks replaces the hunks in the model for reuse in subsequent selections.
// This allows the hunk selector to be reused in a loop without creating new models.
func (m *HunkSelectorModel) SetHunks(hunks []git.Hunk) {
	items := make([]HunkItem, len(hunks))
	fileMap := make(map[string][]int)
	fileOrder := make([]string, 0)
	fileBinary := make(map[string]bool)

	for i, h := range hunks {
		items[i] = HunkItem{
			Hunk:       h,
			Selected:   false,
			Expanded:   false,
			Splittable: !h.Binary && h.CanSplit(),
		}
		if _, exists := fileMap[h.File]; !exists {
			fileOrder = append(fileOrder, h.File)
		}
		fileMap[h.File] = append(fileMap[h.File], i)
		if h.Binary {
			fileBinary[h.File] = true
		}
	}

	fileGroups := make([]FileGroup, len(fileOrder))
	for i, file := range fileOrder {
		fileGroups[i] = FileGroup{
			File:   file,
			Hunks:  fileMap[file],
			Binary: fileBinary[file],
		}
	}

	m.items = items
	m.fileGroups = fileGroups
	m.cursor = 0
	m.Done = false
	m.err = nil

	// Update viewport content if ready
	if m.ready {
		m.updateViewportContent()
	}
}

// Reset resets the model state for reuse
func (m *HunkSelectorModel) Reset() {
	m.Done = false
	m.err = nil
	for i := range m.items {
		m.items[i].Selected = false
		m.items[i].Expanded = false
	}
	m.cursor = 0
	if m.ready {
		m.updateViewportContent()
	}
}

// Err returns any error that occurred
func (m *HunkSelectorModel) Err() error {
	return m.err
}

// PromptSelectHunks shows an interactive hunk selector and returns the selected hunks
func PromptSelectHunks(hunks []git.Hunk) ([]git.Hunk, error) {
	if err := CheckInteractiveAllowed(); err != nil {
		return nil, err
	}

	if len(hunks) == 0 {
		return nil, nil
	}

	m := NewHunkSelectorModel(hunks)

	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	model, err := p.Run()
	if err != nil {
		return nil, err
	}

	if finalModel, ok := model.(*HunkSelectorModel); ok {
		if finalModel.Err() != nil {
			return nil, finalModel.Err()
		}
		return finalModel.SelectedHunks(), nil
	}

	return nil, fmt.Errorf("unexpected model type")
}
