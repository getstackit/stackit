// Package common provides shared helper functions for CLI commands.
package common

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui/style"
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
	return RunWithOptions(cmd, opts, fn)
}

// RunWithOptions is like Run but allows commands to tune context construction.
func RunWithOptions(cmd *cobra.Command, opts app.GlobalOptions, fn func(ctx *app.Context) error) error {
	ctx, err := app.GetContextWithWriter(cmd.Context(), opts, cmd.OutOrStdout())
	if err != nil {
		return err
	}

	// Populate worktree context
	if ctx.Engine != nil && !opts.SkipManagedWorktreeCheck {
		if isManaged, wtInfo, err := ctx.Engine.IsInManagedWorktree(); err == nil && isManaged {
			ctx.InManagedWorktree = true
			ctx.WorktreeInfo = wtInfo
		}
	}

	if err := RunCommandHooks(ctx, cmd, PhasePre); err != nil {
		return HandleCommandError(err)
	}

	err = fn(ctx)
	// Post-hooks fire regardless of fn outcome so users can observe success
	// and failure both. Their errors are surfaced as warnings (non-blocking).
	_ = RunCommandHooks(ctx, cmd, PhasePost)
	if err != nil {
		// The conflict workflow already printed full resolution instructions;
		// silence cobra's "Error: ..." reprint but keep the non-zero exit.
		if errors.Is(err, errors.ErrConflictWorkflow) {
			cmd.SilenceErrors = true
		}
		return HandleCommandError(err)
	}
	return nil
}

// ApplyReadOnlyCurrentBranch tunes opts for commands that only inspect the
// current branch or its parent chain: skip the full metadata bootstrap and
// the managed-worktree check. Compose this when the optimization only applies
// to some code paths (e.g. show vs. write modes of a single command).
func ApplyReadOnlyCurrentBranch(opts app.GlobalOptions) app.GlobalOptions {
	loadMode := engine.LoadModeBranchesOnly
	opts.EngineLoadMode = &loadMode
	opts.SkipManagedWorktreeCheck = true
	return opts
}

// RunReadOnlyCurrentBranch is for commands whose every code path is a
// read-only inspection of the current branch or its parent chain.
func RunReadOnlyCurrentBranch(cmd *cobra.Command, fn func(ctx *app.Context) error) error {
	return RunWithOptions(cmd, ApplyReadOnlyCurrentBranch(GetGlobalOptions(cmd)), fn)
}

// ResolveBranchArg returns the first positional branch argument, or the current
// branch name when no argument was provided.
func ResolveBranchArg(ctx *app.Context, args []string, missingErr error) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	currentBranch := ctx.Engine.CurrentBranch()
	if currentBranch == nil {
		return "", missingErr
	}
	return currentBranch.GetName(), nil
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
	runner := git.NewRunner(nil)
	if cwd != "" {
		runner = git.NewRunnerWithPath(cwd, nil)
	}
	branches, err := runner.GetAllBranchNames(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return branches, cobra.ShellCompDirectiveNoFileComp
}
