package common

import (
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/tui"
)

// TUIPrompter implements handler.Prompter with the shared Bubble Tea prompts.
// It calls through the tui prompt variables at call time so tests that stub
// them (e.g. tui.PromptConfirm) keep working.
type TUIPrompter struct{}

// Confirm implements handler.Confirmer.
func (TUIPrompter) Confirm(message string, defaultYes bool) (bool, error) {
	return tui.PromptConfirm(message, defaultYes)
}

// Select implements handler.Selector.
func (TUIPrompter) Select(title string, options []handler.SelectOption, defaultIndex int) (string, error) {
	tuiOptions := make([]tui.SelectOption, len(options))
	for i, opt := range options {
		tuiOptions[i] = tui.SelectOption{Label: opt.Label, Value: opt.Value}
	}
	return tui.PromptSelect(title, tuiOptions, defaultIndex)
}

// TextInput implements handler.TextInputPrompter.
func (TUIPrompter) TextInput(prompt, defaultValue string) (string, error) {
	return tui.PromptTextInput(prompt, defaultValue)
}

// MultiSelect implements handler.MultiSelector.
func (TUIPrompter) MultiSelect(title string, options []string) ([]string, error) {
	return tui.PromptMultiSelect(title, options)
}

var _ handler.Prompter = TUIPrompter{}
