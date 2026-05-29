package cli

import (
	"fmt"

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
		messageFile string
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
  printf "Auth\n\nOAuth2 impl" | stackit describe -F -   # Read title+body from stdin
  stackit describe -F desc.txt                  # Read title+body from a file
  stackit describe --show                       # Display current description
  stackit describe --clear                      # Remove description`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			title, desc := message, description

			// -F/--message-file supplies the full description in editor format
			// (first line title, blank line, then body). Mutually exclusive with
			// -m/-d so the input source is unambiguous.
			if messageFile != "" {
				if message != "" || description != "" {
					return fmt.Errorf("cannot use --message-file with --title/--description; pass only one")
				}
				content, err := common.ReadMessage("", messageFile)
				if err != nil {
					return err
				}
				parsed := describe.ParseEditorContent(content)
				if parsed == nil {
					return fmt.Errorf("--message-file content has no title (first non-empty line)")
				}
				title, desc = parsed.Title, parsed.Description
			}

			// Only show when explicitly asked. A no-input non-interactive
			// invocation falls through to the action, which errors clearly
			// instead of silently resolving to show (a no-op for a set attempt).
			effectiveShow := show
			globalOpts := common.GetGlobalOptions(cmd)
			if effectiveShow {
				globalOpts = common.ApplyReadOnlyCurrentBranch(globalOpts)
			}

			return common.RunWithOptions(cmd, globalOpts, func(ctx *app.Context) error {
				opts := describe.Options{
					Title:       title,
					Description: desc,
					Clear:       clearFlag,
					Show:        effectiveShow,
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
	cmd.Flags().StringVarP(&messageFile, "message-file", "F", "", "Read the title and description from a file (use \"-\" for stdin): first line is the title, then a blank line, then the body. Mutually exclusive with --title/--description.")
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
