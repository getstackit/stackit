package navigation

import (
	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// NewCheckoutUI creates a runner and handler pair for checkout operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as interactive selection is handled inline.
func NewCheckoutUI(_ output.Output, _ output.Logger) (*tui.Runner, actions.CheckoutHandler) {
	if tui.IsTTY() {
		return nil, &InteractiveCheckoutHandler{}
	}
	return nil, &SimpleCheckoutHandler{}
}

// InteractiveCheckoutHandler provides interactive TUI for TTY environments
type InteractiveCheckoutHandler struct{}

// SelectBranch prompts the user to select a branch using the interactive log selector
func (h *InteractiveCheckoutHandler) SelectBranch(ctx *app.Context, opts actions.CheckoutOptions) (string, error) {
	branchName, err := tui.PromptTreeSelect(ctx.Context, ctx.Engine, ctx.GitHub(), tui.TreeOptions{
		ShowUntracked:  opts.ShowUntracked,
		SkipEnrichment: true, // Skip GitHub/git enrichment for faster startup
		Inline:         true, // Run inline without taking over the terminal
		Logger:         ctx.Logger,
	})
	if err != nil {
		if errors.Is(err, errors.ErrCanceled) {
			// Return empty string to indicate cancellation (not an error)
			return "", nil
		}
		return "", err
	}
	return branchName, nil
}

// SimpleCheckoutHandler provides non-interactive output for non-TTY environments
type SimpleCheckoutHandler struct{}

// SelectBranch returns an error since interactive selection is not available in non-TTY mode
func (h *SimpleCheckoutHandler) SelectBranch(_ *app.Context, _ actions.CheckoutOptions) (string, error) {
	return "", errors.New("interactive branch selection is not available in non-interactive mode; please specify a branch name")
}
