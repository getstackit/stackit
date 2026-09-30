// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/utils"
)

// NewReorderCmd creates the reorder command
func NewReorderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reorder",
		Short: "Reorder branches between trunk and the current branch",
		Long: `Reorder branches between trunk and the current branch, restacking all of their descendants.

Opens an editor where you can reorder branches by moving around a line
corresponding to each branch. After saving and closing the editor, the
branches will be restacked in the new order.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				opts := actions.ReorderOptions{}
				// The Bubble Tea editor needs a TTY; without one the action
				// falls back to editing the order as text in $EDITOR.
				if utils.IsTTY() {
					opts.Editor = tuiOrderEditor{}
				}
				return actions.ReorderAction(ctx, opts)
			})
		},
	}

	return cmd
}

// tuiOrderEditor implements actions.OrderEditor with the Bubble Tea reorder view.
type tuiOrderEditor struct{}

func (tuiOrderEditor) EditOrder(branchesTipFirst []string, trunk string) ([]string, error) {
	newOrder, err := tui.RunReorderTUI(branchesTipFirst, trunk)
	if errors.Is(err, tui.ErrReorderCanceled) {
		return nil, actions.ErrReorderCanceled
	}
	return newOrder, err
}
