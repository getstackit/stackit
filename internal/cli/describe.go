package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/describe"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/utils"
)

// newDescribeCmd creates the describe command
func newDescribeCmd() *cobra.Command {
	var (
		message     string
		description string
		clearFlag   bool
		show        bool
	)

	cmd := &cobra.Command{
		Use:   "describe",
		Short: "Set a title and description for the current stack",
		Long: `Set a title and description for the current stack.

The description is stored on the stack's root branch (the first branch above trunk)
and applies to the entire stack. It can help others understand what the stack is about.

When run without flags, opens your configured editor (like git commit).

Examples:
  stackit describe                              # Opens editor to set/edit description
  stackit describe -m "Auth Feature"            # Set title only (short flag like git commit)
  stackit describe -m "Auth" -d "OAuth2 impl"   # Set title and description
  stackit describe --show                       # Display current description
  stackit describe --clear                      # Remove description`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// If no flags and no message, default to editor (interactive) or show
				if message == "" && !clearFlag && !show && !utils.IsInteractive() {
					show = true
				}

				opts := describe.Options{
					Title:       message,
					Description: description,
					Clear:       clearFlag,
					Show:        show,
				}

				handler := &describeHandler{
					interactive: utils.IsInteractive(),
				}
				return describe.Action(ctx, opts, handler)
			})
		},
	}

	cmd.Flags().StringVarP(&message, "title", "m", "", "Set the stack title (non-interactive)")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Set the stack description body (requires -m)")
	cmd.Flags().BoolVar(&clearFlag, "clear", false, "Remove the stack description")
	cmd.Flags().BoolVar(&show, "show", false, "Display the current stack description")

	return cmd
}

// describeHandler implements describe.Handler
type describeHandler struct {
	interactive bool
}

func (h *describeHandler) Cleanup() {}

func (h *describeHandler) IsInteractive() bool {
	return h.interactive
}
