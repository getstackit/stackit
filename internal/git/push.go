package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// PushOptions applies to every branch in a push. Lease policies belong to PushSpec.
type PushOptions struct {
	Force    bool
	NoVerify bool
}

// PushSpec describes a branch and its lease policy for PushBranches.
type PushSpec struct {
	BranchName string
	LeaseMode  PushLeaseMode
	// ExpectedRemoteSHA is the SHA the remote ref is expected to be at, used for
	// PushLeaseExplicit. An empty value means the branch is not expected to exist
	// on the remote yet (a create); the lease then asserts the ref is absent.
	ExpectedRemoteSHA string
}

// PushLeaseMode selects how a branch is protected against remote changes.
type PushLeaseMode uint8

const (
	// PushLeaseExplicit checks ExpectedRemoteSHA, including an absent remote ref.
	PushLeaseExplicit PushLeaseMode = iota
	// PushLeaseTracking checks the local remote-tracking ref.
	PushLeaseTracking
	// PushLeaseNone uses Git's normal fast-forward checks.
	PushLeaseNone
)

// PushResults holds each requested branch's error, or nil on success.
type PushResults map[string]error

// One extracts a one-branch result without performing I/O.
func (r PushResults) One() error {
	if len(r) != 1 {
		return fmt.Errorf("expected one push result, got %d", len(r))
	}
	for _, err := range r {
		return err
	}
	return nil
}

// PushBranches pushes multiple branches to a remote in a single git invocation,
// replacing N separate `git push` round trips with one. It returns a per-branch
// result map: a nil entry means that branch pushed successfully. Push is
// non-atomic, so a force-with-lease rejection on one branch does not block the
// others — matching the prior one-push-per-branch behavior where each branch
// succeeded or failed independently.
//
// When opts.Force is set, every branch is pushed with --force and the per-branch
// lease policy is ignored. Otherwise each branch uses its own LeaseMode.
func (r *runner) PushBranches(ctx context.Context, remote string, specs []PushSpec, opts PushOptions) PushResults {
	results := make(PushResults, len(specs))
	if len(specs) == 0 {
		return results
	}

	args := []string{gitCmdPush, "--porcelain", "-u", remote}
	if opts.Force {
		args = append(args, "--force")
	} else {
		for _, s := range specs {
			switch s.LeaseMode {
			case PushLeaseExplicit:
				args = append(args, fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", s.BranchName, s.ExpectedRemoteSHA))
			case PushLeaseTracking:
				args = append(args, "--force-with-lease=refs/heads/"+s.BranchName)
			case PushLeaseNone:
				// Git rejects non-fast-forward updates without a force option.
			}
		}
	}
	if opts.NoVerify {
		args = append(args, "--no-verify")
	}
	for _, s := range specs {
		args = append(args, s.BranchName)
	}

	out, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		// `--porcelain` writes its per-ref status to stdout even when the push
		// exits non-zero (some refs rejected); recover it from the error.
		var ce *CommandError
		if errors.As(err, &ce) {
			out = ce.Stdout
		}
	}

	parsePushPorcelain(out, results)

	// Any branch not reported by porcelain (e.g. a connection/auth failure that
	// produced no per-ref lines) inherits the command error so nothing is
	// silently treated as a success.
	for _, s := range specs {
		if _, ok := results[s.BranchName]; ok {
			continue
		}
		if err != nil {
			results[s.BranchName] = fmt.Errorf("failed to push branch %s: %w", s.BranchName, err)
		} else {
			results[s.BranchName] = nil
		}
	}

	return results
}

// parsePushPorcelain parses `git push --porcelain` output into per-branch
// results, writing into the provided map. Each ref line has the form
// "<flag>\t<from>:<to>\t<summary>"; only "!" indicates a rejected ref.
func parsePushPorcelain(out string, results map[string]error) {
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" || strings.HasPrefix(line, "To ") || line == "Done" {
			continue
		}
		flag, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		refs, summary, _ := strings.Cut(rest, "\t")
		_, to, ok := strings.Cut(refs, ":")
		if !ok {
			continue
		}
		branch, ok := strings.CutPrefix(to, "refs/heads/")
		if !ok {
			continue
		}
		if flag == "!" {
			if strings.Contains(summary, "stale info") || strings.Contains(summary, "forced update") {
				results[branch] = fmt.Errorf("%w: force-with-lease push of %s failed due to external changes to the remote branch", ErrStaleRemoteInfo, branch)
			} else {
				results[branch] = fmt.Errorf("failed to push branch %s: %s", branch, strings.TrimSpace(summary))
			}
			continue
		}
		results[branch] = nil
	}
}
