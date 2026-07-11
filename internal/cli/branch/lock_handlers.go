package branch

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions/lock"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewLockUI creates a handler for lock/unlock operations.
// Caller must defer handler.Cleanup() to restore terminal on exit.
func NewLockUI(out output.Output, interactive bool) lock.Handler {
	if interactive {
		return NewInteractiveLockHandler(out)
	}
	return NewSimpleLockHandler(out)
}

// SimpleLockHandler provides non-interactive handling for lock operations
type SimpleLockHandler struct {
	common.BaseHandler
}

// NewSimpleLockHandler creates a new SimpleLockHandler
func NewSimpleLockHandler(out output.Output) *SimpleLockHandler {
	return &SimpleLockHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// PromptSubmitBeforeLock returns false for simple handler (skip submit)
func (h *SimpleLockHandler) PromptSubmitBeforeLock(_ []string) (bool, error) {
	return false, nil
}

// PromptUnlockDownstack returns false for simple handler (skip)
func (h *SimpleLockHandler) PromptUnlockDownstack(_ []string) (bool, error) {
	return false, nil
}

// GetSubmitHandler returns nil for simple handler
func (h *SimpleLockHandler) GetSubmitHandler() submit.Handler {
	return nil
}

// InteractiveLockHandler provides interactive prompts for lock operations
type InteractiveLockHandler struct {
	SimpleLockHandler
}

// NewInteractiveLockHandler creates a new InteractiveLockHandler
func NewInteractiveLockHandler(out output.Output) *InteractiveLockHandler {
	return &InteractiveLockHandler{
		SimpleLockHandler: *NewSimpleLockHandler(out),
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveLockHandler) IsInteractive() bool {
	return true
}

// PromptSubmitBeforeLock prompts user to submit unpushed changes before locking
func (h *InteractiveLockHandler) PromptSubmitBeforeLock(_ []string) (bool, error) {
	return tui.PromptConfirm("Would you like to submit these changes before locking?", true)
}

// PromptUnlockDownstack prompts user to also unlock downstack locked branches
func (h *InteractiveLockHandler) PromptUnlockDownstack(lockedBranchNames []string) (bool, error) {
	var prompt string
	if len(lockedBranchNames) == 1 {
		prompt = fmt.Sprintf("Would you like to also unlock the downstack branch %s?", style.ColorBranchName(lockedBranchNames[0]))
	} else {
		prompt = fmt.Sprintf("Would you like to also unlock %d downstack branches (%s)?", len(lockedBranchNames), strings.Join(lockedBranchNames, ", "))
	}
	return tui.PromptConfirm(prompt, true)
}

// GetSubmitHandler returns a handler for the nested submit operation
func (h *InteractiveLockHandler) GetSubmitHandler() submit.Handler {
	return &lockSubmitHandler{splog: h.Output}
}

// lockSubmitHandler implements submit.Handler for the nested submit operation within lock
type lockSubmitHandler struct {
	splog output.Output
}

func (h *lockSubmitHandler) OnEvent(e submit.Event) {
	if ev, ok := e.(submit.BranchProgressEvent); ok {
		switch ev.Status {
		case submit.StatusDone:
			h.splog.Info("  ✓ %s submitted → %s", ev.BranchName, ev.URL)
		case submit.StatusError:
			h.splog.Warn("  ✗ %s failed: %v", ev.BranchName, ev.Error)
		}
	}
}

func (h *lockSubmitHandler) Confirm(_ string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

func (h *lockSubmitHandler) IsInteractive() bool {
	return false
}
