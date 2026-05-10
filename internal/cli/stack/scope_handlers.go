package stack

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/scope"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// NewScopeUI creates a handler for scope operations.
// Caller must defer handler.Cleanup() to restore terminal on exit.
func NewScopeUI(out output.Output, interactive bool) scope.Handler {
	if interactive {
		return NewInteractiveScopeHandler(out)
	}
	return NewSimpleScopeHandler(out)
}

// SimpleScopeHandler provides non-interactive handling for scope operations
type SimpleScopeHandler struct {
	common.BaseHandler
}

// NewSimpleScopeHandler creates a new SimpleScopeHandler
func NewSimpleScopeHandler(out output.Output) *SimpleScopeHandler {
	return &SimpleScopeHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// PromptConfirmRename returns false for simple handler (non-interactive)
func (h *SimpleScopeHandler) PromptConfirmRename(_, oldScope, newScope string) (bool, error) {
	// In non-interactive mode, print a message but don't rename
	h.Output.Info("Branch name contains '%s', but its scope is now '%s'. Use interactive mode to rename.",
		oldScope, newScope)
	return false, nil
}

// InteractiveScopeHandler provides interactive prompts for scope operations
type InteractiveScopeHandler struct {
	SimpleScopeHandler
}

// NewInteractiveScopeHandler creates a new InteractiveScopeHandler
func NewInteractiveScopeHandler(out output.Output) *InteractiveScopeHandler {
	return &InteractiveScopeHandler{
		SimpleScopeHandler: *NewSimpleScopeHandler(out),
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveScopeHandler) IsInteractive() bool {
	return true
}

// PromptConfirmRename prompts user to confirm branch rename after scope change
func (h *InteractiveScopeHandler) PromptConfirmRename(_, oldScope, newScope string) (bool, error) {
	return tui.PromptConfirm(fmt.Sprintf("Branch name contains '%s', but its scope is now '%s'. Would you like to rename the branch?", oldScope, newScope), true)
}
