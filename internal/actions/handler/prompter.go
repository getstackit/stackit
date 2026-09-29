package handler

import "github.com/getstackit/stackit/internal/utils"

// SelectOption is one choice in a single-select prompt. Label is what the
// user sees; Value is what the prompt returns when the option is chosen.
type SelectOption struct {
	Label string
	Value string
}

// Confirmer asks a yes/no question.
type Confirmer interface {
	// Confirm returns the user's answer. defaultYes is the answer selected
	// when the user just presses Enter.
	Confirm(message string, defaultYes bool) (bool, error)
}

// Selector asks the user to pick one option.
type Selector interface {
	// Select returns the Value of the chosen option. defaultIndex is the
	// option highlighted initially.
	Select(title string, options []SelectOption, defaultIndex int) (string, error)
}

// TextInputPrompter asks the user for a line of text.
type TextInputPrompter interface {
	// TextInput returns the entered text, pre-filled with defaultValue.
	TextInput(prompt, defaultValue string) (string, error)
}

// MultiSelector asks the user to pick any number of options.
type MultiSelector interface {
	// MultiSelect returns the chosen options.
	MultiSelect(title string, options []string) ([]string, error)
}

// Prompter bundles the generic interactive prompts actions may need. Actions
// should depend on the narrowest of the embedded interfaces that covers
// their interaction; adapters (CLI/TUI) provide the implementation.
//
// Implementations must return utils.ErrInteractiveDisabled (possibly
// wrapped) when interactive prompts are not available.
type Prompter interface {
	Confirmer
	Selector
	TextInputPrompter
	MultiSelector
}

// NonInteractivePrompter is the Prompter used when no adapter supplied one.
// Every prompt fails with utils.ErrInteractiveDisabled, matching what an
// interactive prompt does under --no-interactive or without a TTY.
type NonInteractivePrompter struct{}

// Confirm implements Confirmer.
func (NonInteractivePrompter) Confirm(string, bool) (bool, error) {
	return false, utils.ErrInteractiveDisabled
}

// Select implements Selector.
func (NonInteractivePrompter) Select(string, []SelectOption, int) (string, error) {
	return "", utils.ErrInteractiveDisabled
}

// TextInput implements TextInputPrompter.
func (NonInteractivePrompter) TextInput(string, string) (string, error) {
	return "", utils.ErrInteractiveDisabled
}

// MultiSelect implements MultiSelector.
func (NonInteractivePrompter) MultiSelect(string, []string) ([]string, error) {
	return nil, utils.ErrInteractiveDisabled
}

var _ Prompter = NonInteractivePrompter{}

// ConfirmPromptHandler adapts a Confirmer to PromptHandler with a fixed
// default answer.
type ConfirmPromptHandler struct {
	Confirmer  Confirmer
	DefaultYes bool
}

// PromptConfirm implements PromptHandler.
func (h ConfirmPromptHandler) PromptConfirm(message string) (bool, error) {
	return h.Confirmer.Confirm(message, h.DefaultYes)
}

var _ PromptHandler = ConfirmPromptHandler{}
