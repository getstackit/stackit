package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (r *runner) Merge(ctx context.Context, branchName string, opts MergeOptions) error {
	args := []string{"merge"}
	if opts.FFOnly {
		args = append(args, "--ff-only")
	}
	if opts.NoEdit {
		args = append(args, "--no-edit")
	}
	if opts.NoFF {
		args = append(args, "--no-ff")
	}
	if opts.Message != "" {
		args = append(args, "-m", opts.Message)
	}
	args = append(args, branchName)

	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to merge %s: %w", branchName, err)
	}
	return nil
}

// MergeMultiple performs an octopus merge of multiple branches into the current branch.
// This creates a single merge commit with multiple parents.
func (r *runner) MergeMultiple(ctx context.Context, branches []string, opts MergeOptions) error {
	if len(branches) == 0 {
		return nil
	}

	args := []string{"merge"}
	if opts.NoEdit {
		args = append(args, "--no-edit")
	}
	if opts.NoFF {
		args = append(args, "--no-ff")
	}
	if opts.Message != "" {
		args = append(args, "-m", opts.Message)
	}
	args = append(args, branches...)

	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to merge branches: %w", err)
	}
	return nil
}

func (r *runner) IsMergeInProgress(ctx context.Context) bool {
	gitDir := r.getGitDir(ctx)
	mergeHead := filepath.Join(gitDir, "MERGE_HEAD")
	if _, err := os.Stat(mergeHead); err == nil {
		return true
	}
	return false
}

func (r *runner) MergeAbort(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "merge", "--abort")
	if err != nil {
		return fmt.Errorf("merge abort failed: %w", err)
	}
	return nil
}

func (r *runner) GetUnmergedFiles(ctx context.Context) ([]string, error) {
	output, err := r.RunGitCommandWithContext(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return []string{}, nil //nolint:nilerr
	}
	if output == "" {
		return []string{}, nil
	}
	return strings.Split(strings.TrimSpace(output), "\n"), nil
}

func (r *runner) IsMerged(ctx context.Context, branchName, target string) (bool, error) {
	// Get merge base
	mergeBase, err := r.GetMergeBase(ctx, branchName, target)
	if err != nil {
		return false, fmt.Errorf("failed to get merge base: %w", err)
	}

	// Get branch revision
	branchRev, err := r.GetRevision(branchName)
	if err != nil {
		return false, fmt.Errorf("failed to get branch revision: %w", err)
	}

	// If merge base equals branch revision, branch is already merged
	if mergeBase == branchRev {
		return true, nil
	}

	// Use git cherry to check if all commits are in trunk
	// git cherry <trunk> <branch> returns commits that are in branch but not in trunk
	// If empty, all commits are merged
	cherryOutput, err := r.RunGitCommandWithContext(ctx, "cherry", target, branchName)
	if err != nil {
		// If cherry fails, fall back to simpler check
		// Check if branch tip is reachable from trunk
		return r.IsAncestor(ctx, branchRev, target)
	}

	// If cherry output is empty or all lines start with '-', branch is merged
	if cherryOutput == "" {
		return true, nil
	}

	// Check if all commits are marked as merged (lines starting with '-')
	lines := strings.SplitSeq(strings.TrimSpace(cherryOutput), "\n")
	for line := range lines {
		if line != "" && line[0] != '-' {
			// cherry reports unmerged commits; but squash merges combine all branch
			// commits into one new commit, so no individual branch commit has to
			// match. Fall back to aggregate patch-id comparison for the whole branch
			// diff against commits added to the target since the branch diverged.
			return r.isSquashMerged(ctx, branchRev, mergeBase, target)
		}
	}

	return true, nil
}

// isSquashMerged detects whether branchName was squash-merged into target by
// comparing the aggregate branch diff's patch-id against commits reachable from
// target but not from mergeBase (i.e. commits added since the branch diverged).
// A squash merge creates a single commit whose diff matches the branch's
// combined diff even when unrelated target commits mean the full tree differs.
//
// Failure modes are biased toward the safe direction:
//
//   - False negative: if the target rewrote the same files between mergeBase and
//     the squash, or the squash landed further back than the scanned window, the
//     patch-ids differ and this returns false. Callers then rebase the branch and
//     surface any conflict in a throwaway validation worktree — no data loss.
//   - False positive: a match means some target commit has a byte-identical
//     combined diff (after --stable normalization), which means the branch's
//     changes already landed. Treating it as merged is correct in that case; an
//     accidental collision of unrelated changes is not realistic for patch-ids.
//
// The scan is bounded to keep long-lived trunks cheap; exceeding it only costs a
// false negative, never a wrong "merged".
func (r *runner) isSquashMerged(ctx context.Context, branchRev, mergeBase, target string) (bool, error) {
	branchPatchID, err := r.diffPatchID(ctx, mergeBase, branchRev)
	if err != nil {
		return false, nil //nolint:nilerr
	}
	if branchPatchID == "" {
		return false, nil
	}

	// Enumerate commits on target since the merge-base (up to 200
	// commits to bound the scan on long-lived trunks).
	logOutput, err := r.RunGitCommandWithContext(ctx, "log", "--format=%H", "-200", mergeBase+".."+target)
	if err != nil {
		return false, nil //nolint:nilerr
	}

	for commitSHA := range strings.SplitSeq(strings.TrimSpace(logOutput), "\n") {
		commitSHA = strings.TrimSpace(commitSHA)
		if commitSHA == "" {
			continue
		}
		commitPatchID, patchErr := r.commitPatchID(ctx, commitSHA)
		if patchErr != nil {
			continue
		}
		if commitPatchID == branchPatchID {
			return true, nil
		}
	}
	return false, nil
}

func (r *runner) diffPatchID(ctx context.Context, base, head string) (string, error) {
	diffOutput, err := r.RunGitCommandRawWithContext(ctx, "diff", "--no-ext-diff", "--full-index", base, head)
	if err != nil {
		return "", err
	}
	return r.patchID(ctx, diffOutput)
}

func (r *runner) commitPatchID(ctx context.Context, commitSHA string) (string, error) {
	diffOutput, err := r.RunGitCommandRawWithContext(ctx, "show", "--format=", "--no-ext-diff", "--full-index", commitSHA)
	if err != nil {
		return "", err
	}
	return r.patchID(ctx, diffOutput)
}

func (r *runner) patchID(ctx context.Context, diffOutput string) (string, error) {
	if strings.TrimSpace(diffOutput) == "" {
		return "", nil
	}
	output, err := r.runGitInternal(ctx, diffOutput, nil, true, "patch-id", "--stable")
	if err != nil {
		return "", err
	}
	patchID, _, _ := strings.Cut(strings.TrimSpace(output), " ")
	return patchID, nil
}
