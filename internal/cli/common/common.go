// Package common provides shared helper functions for CLI commands.
package common

import (
	"fmt"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui/style"
)

// GetGlobalOptions returns runtime.GlobalOptions populated from a cobra.Command's flags
func GetGlobalOptions(cmd *cobra.Command) app.GlobalOptions {
	interactive, _ := cmd.Flags().GetBool("interactive")
	verify, _ := cmd.Flags().GetBool("verify")
	debug, _ := cmd.Flags().GetBool("debug")
	quiet, _ := cmd.Flags().GetBool("quiet")
	cwd, _ := cmd.Flags().GetString("cwd")

	return app.GlobalOptions{
		Interactive: interactive,
		Verify:      verify,
		Debug:       debug,
		Quiet:       quiet,
		Cwd:         cwd,
	}
}

// Run is a helper that provides a runtime context to a command's execution function
func Run(cmd *cobra.Command, fn func(ctx *app.Context) error) error {
	opts := GetGlobalOptions(cmd)
	ctx, err := app.GetContextWithWriter(cmd.Context(), opts, cmd.OutOrStdout())
	if err != nil {
		return err
	}
	err = fn(ctx)
	if err != nil {
		return HandleCommandError(err)
	}
	return nil
}

// HandleCommandError formats known error types for user display.
func HandleCommandError(err error) error {
	if errors.Is(err, errors.ErrCanceled) {
		return nil
	}
	var modErr *errors.BranchModificationError
	if errors.As(err, &modErr) {
		return fmt.Errorf("%s", style.FormatBranchModificationError(modErr))
	}
	return err
}

// CompleteBranches is a helper for cobra.ValidArgsFunction and RegisterFlagCompletionFunc
// that returns all branch names in the repository.
func CompleteBranches(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	cwd, _ := cmd.Flags().GetString("cwd")
	runner := git.NewRunner()
	if cwd != "" {
		runner = git.NewRunnerWithPath(cwd)
	}
	branches, err := runner.GetAllBranchNames()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return branches, cobra.ShellCompDirectiveNoFileComp
}
