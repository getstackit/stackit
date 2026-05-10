package stack

import (
	"github.com/getstackit/stackit/internal/actions/navigation"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// NewNavigationUI creates a handler for navigation operations.
// Caller must defer handler.Cleanup() to restore terminal on exit.
func NewNavigationUI(out output.Output, interactive bool) navigation.Handler {
	if interactive {
		return NewInteractiveNavigationHandler(out)
	}
	return NewSimpleNavigationHandler(out)
}

// SimpleNavigationHandler provides non-interactive handling for navigation operations
type SimpleNavigationHandler struct {
	common.BaseHandler
}

// NewSimpleNavigationHandler creates a new SimpleNavigationHandler
func NewSimpleNavigationHandler(out output.Output) *SimpleNavigationHandler {
	return &SimpleNavigationHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// PromptSelectBranch returns an empty string for simple handler (non-interactive)
// The action will return an error listing available branches
func (h *SimpleNavigationHandler) PromptSelectBranch(_ string, _ []string) (string, error) {
	return "", nil
}

// InteractiveNavigationHandler provides interactive prompts for navigation operations
type InteractiveNavigationHandler struct {
	SimpleNavigationHandler
}

// NewInteractiveNavigationHandler creates a new InteractiveNavigationHandler
func NewInteractiveNavigationHandler(out output.Output) *InteractiveNavigationHandler {
	return &InteractiveNavigationHandler{
		SimpleNavigationHandler: *NewSimpleNavigationHandler(out),
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveNavigationHandler) IsInteractive() bool {
	return true
}

// PromptSelectBranch prompts user to select a branch from the list
func (h *InteractiveNavigationHandler) PromptSelectBranch(message string, branches []string) (string, error) {
	options := make([]tui.SelectOption, len(branches))
	for i, branch := range branches {
		options[i] = tui.SelectOption{
			Label: branch,
			Value: branch,
		}
	}

	return tui.PromptSelect(message, options, 0)
}
