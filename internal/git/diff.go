package git

import (
	"context"
	"fmt"
	"slices"
)

func (r *runner) IsDiffEmpty(_ context.Context, branchName, base string) (bool, error) {
	branchRev, err := r.GetRevision(branchName)
	if err != nil {
		return false, fmt.Errorf("failed to get branch revision: %w", err)
	}

	if branchRev == base {
		return true, nil
	}

	// Two refs at different commits can still represent identical content if
	// their trees match (e.g. revert chain, no-op rebase). Compare commit
	// TreeHash directly instead of walking the file-level diff — go-git tree
	// hashes are content-addressed, so equal hash ⇔ no changes. This skips
	// the O(tree-size) DiffContext call that dominated branch-deletion
	// planning.
	repo, err := r.ensureRepo()
	if err != nil {
		return false, err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	baseHash, err := r.resolveRefHashInternal(repo, base)
	if err != nil {
		return false, fmt.Errorf("failed to resolve base %s: %w", base, err)
	}
	headHash, err := r.resolveRefHashInternal(repo, branchRev)
	if err != nil {
		return false, fmt.Errorf("failed to resolve head %s: %w", branchRev, err)
	}
	baseCommit, err := repo.CommitObject(baseHash)
	if err != nil {
		return false, fmt.Errorf("failed to load base commit %s: %w", base, err)
	}
	headCommit, err := repo.CommitObject(headHash)
	if err != nil {
		return false, fmt.Errorf("failed to load head commit %s: %w", branchRev, err)
	}
	return baseCommit.TreeHash == headCommit.TreeHash, nil
}

func (r *runner) GetChangedFiles(ctx context.Context, base, head string) ([]string, error) {
	files, err := r.changedFilesBetween(ctx, base, head)
	if err != nil {
		return nil, fmt.Errorf("failed to get changed files: %w", err)
	}
	return files, nil
}

func (r *runner) changedFilesBetween(ctx context.Context, base, head string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	repo, err := r.ensureRepo()
	if err != nil {
		return nil, err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	baseHash, err := r.resolveRefHashInternal(repo, base)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base %s: %w", base, err)
	}
	headHash, err := r.resolveRefHashInternal(repo, head)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve head %s: %w", head, err)
	}

	baseCommit, err := repo.CommitObject(baseHash)
	if err != nil {
		return nil, fmt.Errorf("failed to load base commit %s: %w", base, err)
	}
	headCommit, err := repo.CommitObject(headHash)
	if err != nil {
		return nil, fmt.Errorf("failed to load head commit %s: %w", head, err)
	}

	baseTree, err := baseCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to load base tree %s: %w", base, err)
	}
	headTree, err := headCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to load head tree %s: %w", head, err)
	}
	if baseTree.Hash == headTree.Hash {
		return []string{}, nil
	}

	changes, err := baseTree.DiffContext(ctx, headTree)
	if err != nil {
		return nil, fmt.Errorf("failed to diff trees: %w", err)
	}
	if len(changes) == 0 {
		return []string{}, nil
	}

	files := make([]string, 0, len(changes))
	for _, change := range changes {
		switch {
		case change.To.Name != "":
			files = append(files, change.To.Name)
		case change.From.Name != "":
			files = append(files, change.From.Name)
		}
	}
	slices.Sort(files)
	return files, nil
}

func (r *runner) ShowDiff(ctx context.Context, left, right string, stat bool) (string, error) {
	args := []string{"-c", "color.ui=always", "--no-pager", "diff", "--no-ext-diff"}
	if stat {
		args = append(args, "--stat")
	}
	args = append(args, left, right, "--")
	return r.RunGitCommandWithContext(ctx, args...)
}

func (r *runner) ShowCommits(ctx context.Context, base, head string, patch, stat bool) (string, error) {
	args := []string{"-c", "color.ui=always", "--no-pager", "log"}
	switch {
	case patch && stat:
		args = append(args, "--stat")
	case patch:
		args = append(args, "-p")
	default:
		args = append(args, "--pretty=format:%h - %s")
	}

	// If base is empty, use head~ (parent commit) for trunk
	baseRef := base
	if base == "" {
		baseRef = head + "~"
	}
	args = append(args, fmt.Sprintf("%s..%s", baseRef, head))
	args = append(args, "--")
	return r.RunGitCommandWithContext(ctx, args...)
}

func (r *runner) GetStagedDiff(ctx context.Context, files ...string) (string, error) {
	args := []string{"diff", "--cached"}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	return r.RunGitCommandRawWithContext(ctx, args...)
}

func (r *runner) GetUnstagedDiff(ctx context.Context, files ...string) (string, error) {
	args := []string{"diff"}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	return r.RunGitCommandRawWithContext(ctx, args...)
}

// GetDiffBetween returns the raw diff between two refs.
// Unlike ShowDiff, this returns uncolored output suitable for parsing.
func (r *runner) GetDiffBetween(ctx context.Context, base, head string, files ...string) (string, error) {
	args := []string{"diff", base, head}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	return r.RunGitCommandRawWithContext(ctx, args...)
}
