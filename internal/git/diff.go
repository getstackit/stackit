package git

import (
	"context"
	"fmt"
)

// RevRange is a base..head revision range. Functions that walk or diff a
// range take it instead of two adjacent strings so the direction is explicit
// at every call site — transposed base/head reads wrong instead of silently
// reversing the diff.
type RevRange struct {
	Base string
	Head string
}

// String returns the git "base..head" range notation.
func (r RevRange) String() string {
	return r.Base + ".." + r.Head
}

func (r *runner) ShowDiff(ctx context.Context, left, right string, stat bool) (string, error) {
	args := []string{"-c", "color.ui=always", "--no-pager", gitCmdDiff, "--no-ext-diff"}
	if stat {
		args = append(args, "--stat")
	}
	args = append(args, left, right, "--")
	return r.RunGitCommandWithContext(ctx, args...)
}

func (r *runner) ShowCommits(ctx context.Context, rr RevRange, patch, stat bool) (string, error) {
	base, head := rr.Base, rr.Head
	args := []string{"-c", "color.ui=always", "--no-pager", gitCmdLog}
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
	args := []string{gitCmdDiff, "--cached"}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	return r.RunGitCommandRawWithContext(ctx, args...)
}

func (r *runner) GetUnstagedDiff(ctx context.Context, files ...string) (string, error) {
	args := []string{gitCmdDiff}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	return r.RunGitCommandRawWithContext(ctx, args...)
}

// GetUnstagedDiffBinary returns the unstaged (index->worktree) diff with full
// binary content (`git diff --binary`). Plain `git diff` emits only a
// "Binary files ... differ" placeholder for binary files, which `git apply`
// refuses ("cannot apply binary patch ... without full index line"). The
// `--binary` form embeds a `GIT binary patch` that round-trips through
// `git apply`, so callers that reapply the captured diff to the working tree
// (e.g. absorb's stash fallback) must use this instead of GetUnstagedDiff.
func (r *runner) GetUnstagedDiffBinary(ctx context.Context, files ...string) (string, error) {
	args := []string{gitCmdDiff, "--binary"}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	return r.RunGitCommandRawWithContext(ctx, args...)
}

// GetDiffBetween returns the raw diff between two refs.
// Unlike ShowDiff, this returns uncolored output suitable for parsing.
func (r *runner) GetDiffBetween(ctx context.Context, rr RevRange, files ...string) (string, error) {
	base, head := rr.Base, rr.Head
	args := []string{gitCmdDiff, base, head}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	return r.RunGitCommandRawWithContext(ctx, args...)
}
