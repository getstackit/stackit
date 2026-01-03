package git

import (
	"context"
	"fmt"
	"strings"
)

// PushOptions contains options for pushing a branch
type PushOptions struct {
	Force          bool
	ForceWithLease bool
	NoVerify       bool
}

func (r *runner) PushBranch(ctx context.Context, branchName, remote string, opts PushOptions) error {
	args := []string{"push", "-u", remote}

	if opts.Force {
		args = append(args, "--force")
	} else if opts.ForceWithLease {
		args = append(args, "--force-with-lease")
	}

	if opts.NoVerify {
		args = append(args, "--no-verify")
	}

	args = append(args, branchName)

	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		if strings.Contains(err.Error(), "stale info") || strings.Contains(err.Error(), "forced update") {
			return fmt.Errorf("%w: force-with-lease push of %s failed due to external changes to the remote branch", ErrStaleRemoteInfo, branchName)
		}
		return fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return nil
}
