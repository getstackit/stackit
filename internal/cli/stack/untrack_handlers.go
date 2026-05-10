package stack

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/untrack"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewUntrackUI creates a handler for untrack operations.
// Caller must defer handler.Cleanup() to restore terminal on exit.
func NewUntrackUI(out output.Output, interactive bool) untrack.Handler {
	if interactive {
		return NewInteractiveUntrackHandler(out)
	}
	return NewSimpleUntrackHandler(out)
}

// SimpleUntrackHandler provides non-interactive handling for untrack operations
type SimpleUntrackHandler struct {
	common.BaseHandler
}

// NewSimpleUntrackHandler creates a new SimpleUntrackHandler
func NewSimpleUntrackHandler(out output.Output) *SimpleUntrackHandler {
	return &SimpleUntrackHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// PromptConfirmUntrackDescendants returns true for simple handler (auto-confirm)
func (h *SimpleUntrackHandler) PromptConfirmUntrackDescendants(_ string, _ int) (bool, error) {
	return true, nil
}

// InteractiveUntrackHandler provides interactive prompts for untrack operations
type InteractiveUntrackHandler struct {
	SimpleUntrackHandler
}

// NewInteractiveUntrackHandler creates a new InteractiveUntrackHandler
func NewInteractiveUntrackHandler(out output.Output) *InteractiveUntrackHandler {
	return &InteractiveUntrackHandler{
		SimpleUntrackHandler: *NewSimpleUntrackHandler(out),
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveUntrackHandler) IsInteractive() bool {
	return true
}

// PromptConfirmUntrackDescendants prompts user to confirm untracking descendants
func (h *InteractiveUntrackHandler) PromptConfirmUntrackDescendants(branchName string, descendantCount int) (bool, error) {
	message := fmt.Sprintf("Branch %s has %d tracked descendants. Untrack all of them?",
		style.ColorBranchName(branchName, false), descendantCount)
	options := []tui.SelectOption{
		{Label: "Yes", Value: "yes"},
		{Label: "No", Value: "no"},
	}

	selected, err := tui.PromptSelect(message, options, 0)
	if err != nil {
		return false, err
	}

	return selected == "yes", nil
}
