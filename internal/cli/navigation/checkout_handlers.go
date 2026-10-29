package navigation

import (
	"context"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/checkout"
	"github.com/getstackit/stackit/internal/actions/navigation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/term"
	"github.com/getstackit/stackit/internal/tui"
)

// navigationDeps builds the top/bottom navigation action's collaborators from
// the runtime context.
func navigationDeps(ctx *app.Context) navigation.Deps {
	return navigation.Deps{
		Engine: ctx.Engine,
		Out:    ctx.Output,
		Log:    ctx.Logger,
		Quiet:  ctx.Quiet,
		Here:   actions.HereFrom(ctx),
	}
}

// NewCheckoutUI creates a runner and handler pair for checkout operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as interactive selection is handled inline.
func NewCheckoutUI(appCtx *app.Context) (*tui.Runner, checkout.Handler) {
	if term.IsInteractive() {
		return nil, &InteractiveCheckoutHandler{app: appCtx}
	}
	return nil, &SimpleCheckoutHandler{}
}

// InteractiveCheckoutHandler provides interactive TUI for TTY environments
type InteractiveCheckoutHandler struct {
	// app supplies the engine, logger and (lazily) the GitHub client the
	// selector reads.
	app *app.Context
}

// SelectBranch prompts the user to select a branch using the interactive log selector
func (h *InteractiveCheckoutHandler) SelectBranch(ctx context.Context, opts checkout.Options) (string, error) {
	branchName, err := tui.PromptTreeSelect(ctx, h.app.Engine, h.app.GitHub(), tui.TreeOptions{
		ShowUntracked:  opts.ShowUntracked,
		SkipEnrichment: true, // Skip GitHub/git enrichment for faster startup
		Inline:         true, // Run inline without taking over the terminal
		Logger:         h.app.Logger,
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
func (h *SimpleCheckoutHandler) SelectBranch(context.Context, checkout.Options) (string, error) {
	return "", errors.New("interactive branch selection is not available in non-interactive mode; please specify a branch name")
}
