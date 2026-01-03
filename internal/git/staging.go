package git

import (
	"context"
	"fmt"
	"strings"
)

func (r *runner) StageAll(ctx context.Context) error {
	_, err := r.runGitCommandWithContextInternal(ctx, "add", "-A")
	if err != nil {
		return fmt.Errorf("failed to stage all changes: %w", err)
	}
	return nil
}

func (r *runner) StagePatch(_ context.Context) error {
	return r.RunGitCommandInteractive("add", "-p")
}

func (r *runner) StageTracked(ctx context.Context) error {
	_, err := r.runGitCommandWithContextInternal(ctx, "add", "-u")
	if err != nil {
		return fmt.Errorf("failed to stage tracked changes: %w", err)
	}
	return nil
}

func (r *runner) AddAll(ctx context.Context) error {
	return r.StageAll(ctx)
}

func (r *runner) StageChanges(ctx context.Context, opts StagingOptions) error {
	if opts.Patch && !opts.All {
		return r.RunGitCommandInteractive("add", "-p")
	}

	if opts.All {
		return r.StageAll(ctx)
	}

	if opts.Update {
		_, err := r.runGitCommandWithContextInternal(ctx, "add", "-u")
		return err
	}

	return nil
}

func (r *runner) HasStagedChanges(ctx context.Context) (bool, error) {
	output, err := r.runGitCommandWithContextInternal(ctx, "diff", "--cached", "--shortstat")
	if err != nil {
		return false, fmt.Errorf("failed to check staged changes: %w", err)
	}
	return strings.TrimSpace(output) != "", nil
}

func (r *runner) HasUnstagedChanges(ctx context.Context) (bool, error) {
	// Use git diff to check for unstaged changes to tracked files
	// This is more reliable than parsing porcelain output which gets trimmed
	output, err := r.runGitCommandWithContextInternal(ctx, "diff", "--name-only")
	if err != nil {
		return false, fmt.Errorf("failed to check unstaged changes: %w", err)
	}
	return strings.TrimSpace(output) != "", nil
}

func (r *runner) HasUntrackedFiles(ctx context.Context) (bool, error) {
	output, err := r.runGitCommandWithContextInternal(ctx, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return false, fmt.Errorf("failed to check for untracked files: %w", err)
	}
	return strings.TrimSpace(output) != "", nil
}

func (r *runner) ParseStagedHunks(ctx context.Context) ([]Hunk, error) {
	diffOutput, err := r.runGitCommandRawWithContextInternal(ctx, "diff", "--cached")
	if err != nil {
		return nil, fmt.Errorf("failed to get staged diff: %w", err)
	}

	return ParseDiffOutput(diffOutput)
}
