package git

import (
	"context"

	"stackit.dev/stackit/internal/utils"
)

// CommitOptions contains options for creating a commit
type CommitOptions struct {
	Message     string
	Amend       bool
	NoEdit      bool
	Edit        bool
	Verbose     int
	ResetAuthor bool
	NoVerify    bool
}

func (r *runner) CommitWithOptions(opts CommitOptions) error {
	args := []string{"commit"}

	if opts.Amend {
		args = append(args, "--amend")
	}

	if opts.NoVerify {
		args = append(args, "--no-verify")
	}

	if opts.ResetAuthor {
		args = append(args, "--reset-author")
	}

	if opts.Verbose > 0 {
		args = append(args, "-v")
	}

	if opts.Message != "" {
		args = append(args, "-m", opts.Message)
	}

	if opts.NoEdit {
		args = append(args, "--no-edit")
	} else if opts.Edit {
		// Only add -e if explicitly requested (git opens editor by default if no message)
		args = append(args, "-e")
	}
	// If we're in non-interactive mode, or if we have a message and aren't explicitly editing,
	// use the non-interactive runner.
	if !utils.IsInteractive() || (opts.Message != "" && !opts.Edit) || (opts.Amend && opts.NoEdit) {
		_, err := r.runGitCommandWithContextInternal(context.Background(), args...)
		return err
	}

	return r.RunGitCommandInteractive(args...)
}

func (r *runner) Commit(message string, verbose int, noVerify bool) error {
	return r.CommitWithOptions(CommitOptions{
		Message:  message,
		Verbose:  verbose,
		NoVerify: noVerify,
	})
}

func (r *runner) CommitAmendNoEdit(ctx context.Context) error {
	_, err := r.runGitCommandWithContextInternal(ctx, "commit", "-a", "--amend", "--no-edit", "--no-verify")
	return err
}
