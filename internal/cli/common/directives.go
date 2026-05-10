package common

import (
	"os"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/output"
)

// HasShellIntegration checks if stackit shell integration is installed.
// The shell wrapper sets STACKIT_SHELL_INTEGRATION=1 when running commands.
func HasShellIntegration() bool {
	return os.Getenv("STACKIT_SHELL_INTEGRATION") == "1"
}

// HandleCheckoutResult handles the worktree switch result from CheckoutAction.
// Returns true if shell integration handled the switch (caller should return nil).
// Returns false with tips printed if no shell integration.
func HandleCheckoutResult(out output.Output, result actions.CheckoutResult) bool {
	if result.WorktreeSwitchPath == "" {
		return false
	}
	if HasShellIntegration() {
		out.DirectiveCD(result.WorktreeSwitchPath)
		if len(result.RerunArgs) > 0 {
			out.DirectiveRerun(result.RerunArgs...)
		}
		return true
	}
	for _, tip := range result.FallbackTips {
		out.Tip("%s", tip)
	}
	return false
}

// CompleteCheckout handles a CheckoutAction result, including the fallback
// checkout path when shell integration cannot switch worktrees for the caller.
func CompleteCheckout(ctx *app.Context, result actions.CheckoutResult, fallbackOpts actions.CheckoutOptions, handler actions.CheckoutHandler) error {
	if HandleCheckoutResult(ctx.Output, result) {
		return nil
	}
	if result.WorktreeSwitchPath == "" {
		return nil
	}

	if !fallbackOpts.CheckoutTrunk {
		fallbackOpts.BranchName = result.TargetBranch
	}
	fallbackOpts.SkipWorktreeSwitch = true
	_, err := actions.CheckoutAction(ctx, fallbackOpts, handler)
	return err
}

// Checkout runs CheckoutAction and completes any worktree-switch fallback.
func Checkout(ctx *app.Context, opts actions.CheckoutOptions, handler actions.CheckoutHandler) (actions.CheckoutResult, error) {
	result, err := actions.CheckoutAction(ctx, opts, handler)
	if err != nil {
		return actions.CheckoutResult{}, err
	}
	return result, CompleteCheckout(ctx, result, opts, handler)
}
