package split

import (
	"github.com/charmbracelet/bubbles/key"

	"stackit.dev/stackit/internal/tui/core"
)

// KeyMap defines unified key bindings for the split wizard
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Submit key.Binding
	Yes    key.Binding
	No     key.Binding
	Cancel key.Binding
}

// ShortHelp returns key bindings for navigation help
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Cancel}
}

// ConfirmHelp returns key bindings for confirmation prompts
func (k KeyMap) ConfirmHelp() []key.Binding {
	return []key.Binding{k.Yes, k.No, k.Cancel}
}

// SubmitHelp returns key bindings for submit prompts
func (k KeyMap) SubmitHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Cancel}
}

// FullHelp returns all key bindings for full help display
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Select, k.Cancel}}
}

// DefaultKeyMap contains the default key bindings for the split wizard
var DefaultKeyMap = KeyMap{
	Up: key.NewBinding(
		key.WithKeys(core.KeyUp, "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys(core.KeyDown, "j"),
		key.WithHelp("↓/j", "down"),
	),
	Select: key.NewBinding(
		key.WithKeys(core.KeyEnter),
		key.WithHelp("enter", "select"),
	),
	Submit: key.NewBinding(
		key.WithKeys(core.KeyEnter),
		key.WithHelp("enter", "submit"),
	),
	Yes: key.NewBinding(
		key.WithKeys("y", core.KeyEnter),
		key.WithHelp("y", "yes"),
	),
	No: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "no"),
	),
	Cancel: key.NewBinding(
		key.WithKeys(core.KeyCtrlC, core.KeyEsc),
		key.WithHelp("esc", "cancel"),
	),
}

// navigationKeyMap is a help.KeyMap adapter for navigation contexts
type navigationKeyMap struct {
	keys KeyMap
}

func (n navigationKeyMap) ShortHelp() []key.Binding {
	return n.keys.ShortHelp()
}

func (n navigationKeyMap) FullHelp() [][]key.Binding {
	return n.keys.FullHelp()
}

// confirmKeyMap is a help.KeyMap adapter for confirmation contexts
type confirmKeyMap struct {
	keys KeyMap
}

func (c confirmKeyMap) ShortHelp() []key.Binding {
	return c.keys.ConfirmHelp()
}

func (c confirmKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{c.keys.ConfirmHelp()}
}

// submitKeyMap is a help.KeyMap adapter for submit contexts
type submitKeyMap struct {
	keys KeyMap
}

func (s submitKeyMap) ShortHelp() []key.Binding {
	return s.keys.SubmitHelp()
}

func (s submitKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{s.keys.SubmitHelp()}
}
