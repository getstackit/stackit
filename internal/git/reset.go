package git

import (
	"context"
	"fmt"

	gogit "github.com/go-git/go-git/v6"
)

func (r *runner) ResetMerge(ctx context.Context, revision string) error {
	err := r.resetWorktree(ctx, revision, gogit.MergeReset)
	if err != nil {
		return fmt.Errorf("failed to reset --merge to %s: %w", revision, err)
	}
	r.revisionCache.InvalidateAll()
	return nil
}

func (r *runner) HardReset(ctx context.Context, revision string) error {
	err := r.resetWorktree(ctx, revision, gogit.HardReset)
	if err != nil {
		return fmt.Errorf("failed to hard reset to %s: %w", revision, err)
	}
	r.revisionCache.InvalidateAll()
	return nil
}

func (r *runner) SoftReset(ctx context.Context, revision string) error {
	err := r.resetWorktree(ctx, revision, gogit.SoftReset)
	if err != nil {
		return fmt.Errorf("failed to soft reset to %s: %w", revision, err)
	}
	r.revisionCache.InvalidateAll()
	return nil
}

func (r *runner) MixedReset(ctx context.Context, revision string) error {
	err := r.resetWorktree(ctx, revision, gogit.MixedReset)
	if err == nil {
		r.revisionCache.InvalidateAll()
	}
	return err
}

func (r *runner) resetWorktree(_ context.Context, revision string, mode gogit.ResetMode) error {
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	hash, err := r.resolveRefHashInternal(repo, revision)
	if err != nil {
		return err
	}

	return worktree.Reset(&gogit.ResetOptions{
		Commit: hash,
		Mode:   mode,
	})
}
