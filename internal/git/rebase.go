package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RebaseResult represents the result of a rebase operation
type RebaseResult int

const (
	// RebaseDone indicates the rebase was successful
	RebaseDone RebaseResult = iota
	// RebaseConflict indicates a conflict occurred during rebase
	RebaseConflict
)

// RebaseOutcome represents the result of a rebase operation, including any
// conflicts that git rerere resolved automatically.
type RebaseOutcome struct {
	Result              RebaseResult
	RerereResolvedCount int
}

func (r *runner) Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RebaseOutcome, error) {
	outcome := RebaseOutcome{Result: RebaseDone}

	// Use detached HEAD to avoid "already used by worktree" errors
	// We use branchName~0 to force a detached checkout of the branch tip
	_, err := r.RunGitCommandWithContext(ctx, "rebase", "--onto", upstream, oldUpstream, branchName+"~0")
	if err != nil {
		r.revisionCache.InvalidateAll()
		if r.IsRebaseInProgress(ctx) {
			autoOutcome, autoErr := r.continueRerereResolvedRebase(ctx, err)
			if autoErr != nil || autoOutcome.Result == RebaseConflict {
				return autoOutcome, autoErr
			}
			outcome = autoOutcome
		} else {
			// Abort rebase if it failed for other reasons
			_, _ = r.RunGitCommandWithContext(ctx, "rebase", "--abort")

			return RebaseOutcome{Result: RebaseConflict}, err
		}
	}

	r.revisionCache.InvalidateAll()

	// Since we rebased in detached HEAD, we must manually update the branch ref
	newRev, err := r.GetCurrentRevision(ctx)
	if err != nil {
		return RebaseOutcome{Result: RebaseConflict, RerereResolvedCount: outcome.RerereResolvedCount}, fmt.Errorf("failed to get revision after rebase: %w", err)
	}

	if err := r.UpdateBranchRef(ctx, branchName, newRev); err != nil {
		return RebaseOutcome{Result: RebaseConflict, RerereResolvedCount: outcome.RerereResolvedCount}, fmt.Errorf("failed to update branch ref %s: %w", branchName, err)
	}

	return outcome, nil
}

func (r *runner) rebaseContinueOnce(ctx context.Context) error {
	_, err := r.RunGitCommandWithEnv(ctx, []string{"GIT_EDITOR=true"}, "rebase", "--continue")
	r.revisionCache.InvalidateAll()
	return err
}

// MaxRerereContinueIterations caps the auto-continue loop so a pathological
// rebase cannot spin forever. Real rebases finish in far fewer iterations.
const MaxRerereContinueIterations = 1000

// AutoContinueRerereRebase drives `git rebase --continue` while rerere keeps
// all conflicts resolved (no unmerged files remain). It returns:
//   - RebaseOutcome with Result=RebaseDone and the count of rerere-resolved
//     commits when the rebase completes.
//   - RebaseOutcome with Result=RebaseConflict and the list of unmerged files
//     when rerere cannot resolve a conflict.
//   - a non-nil error if `rebase --continue` fails unexpectedly or the
//     iteration cap is hit. The originalErr is wrapped into these errors so
//     callers keep the context that triggered auto-continue.
func AutoContinueRerereRebase(ctx context.Context, r Runner, originalErr error) (RebaseOutcome, []string, error) {
	outcome := RebaseOutcome{Result: RebaseConflict}
	skipNextContinueCount := false

	for range MaxRerereContinueIterations {
		if !r.IsRebaseInProgress(ctx) {
			outcome.Result = RebaseDone
			return outcome, nil, nil
		}

		unmergedFiles, err := applyRerereToUnmerged(ctx, r)
		if err != nil {
			return outcome, nil, originalErr
		}
		if len(unmergedFiles) > 0 {
			return outcome, unmergedFiles, nil
		}

		if _, err := r.RunGitCommandWithEnv(ctx, []string{"GIT_EDITOR=true"}, "rebase", "--continue"); err != nil {
			if isRebaseContinueStagedChangesError(err) && r.IsRebaseInProgress(ctx) {
				unmergedFiles, filesErr := r.GetUnmergedFiles(ctx)
				if filesErr == nil && len(unmergedFiles) == 0 {
					if _, commitErr := r.RunGitCommandWithEnv(ctx, []string{"GIT_EDITOR=true"}, "commit"); commitErr != nil {
						return outcome, nil, fmt.Errorf("commit after rerere replay failed: %w", commitErr)
					}
					outcome.RerereResolvedCount++
					skipNextContinueCount = true
					continue
				}
			}
			if r.IsRebaseInProgress(ctx) {
				// rerere.autoupdate may not have staged the replayed
				// resolution by the time `rebase --continue` returns, so give
				// rerere the same explicit chance to apply (and, with
				// autoupdate, stage) a known resolution as the top of the loop
				// before treating this as an unresolved conflict.
				unmergedFiles, filesErr := applyRerereToUnmerged(ctx, r)
				if filesErr == nil {
					if len(unmergedFiles) > 0 {
						return outcome, unmergedFiles, nil
					}
					outcome.RerereResolvedCount++
					continue
				}
			}
			return outcome, nil, fmt.Errorf("rebase --continue failed after rerere replay: %w", err)
		}
		if skipNextContinueCount {
			skipNextContinueCount = false
		} else {
			outcome.RerereResolvedCount++
		}
	}

	return outcome, nil, fmt.Errorf("rerere auto-continue exceeded %d iterations: %w", MaxRerereContinueIterations, originalErr)
}

// applyRerereToUnmerged replays any recorded rerere resolutions for the
// currently-conflicted paths (staging them when rerere.autoupdate is on) and
// returns the paths that remain unmerged afterwards. It is a no-op when nothing
// is unmerged. Callers use the returned slice to decide whether a conflict is
// genuinely unresolved: a non-empty result means rerere had no resolution to
// apply (or one that did not fully clear the conflict).
func applyRerereToUnmerged(ctx context.Context, r Runner) ([]string, error) {
	unmerged, err := r.GetUnmergedFiles(ctx)
	if err != nil {
		return nil, err
	}
	if len(unmerged) == 0 {
		return unmerged, nil
	}
	// A failed `git rerere` simply means no resolution was applied; keep the
	// current unmerged set so the caller treats it as a genuine conflict.
	if _, rerereErr := r.RunGitCommandWithContext(ctx, "rerere"); rerereErr == nil {
		refreshed, refreshErr := r.GetUnmergedFiles(ctx)
		if refreshErr != nil {
			return nil, refreshErr
		}
		unmerged = refreshed
	}
	return unmerged, nil
}

func isRebaseContinueStagedChangesError(err error) bool {
	var commandErr *CommandError
	if errors.As(err, &commandErr) {
		return strings.Contains(commandErr.Stderr, "you have staged changes in your working tree")
	}
	return strings.Contains(err.Error(), "you have staged changes in your working tree")
}

func (r *runner) continueRerereResolvedRebase(ctx context.Context, originalErr error) (RebaseOutcome, error) {
	outcome, _, err := AutoContinueRerereRebase(ctx, r, originalErr)
	r.revisionCache.InvalidateAll()
	return outcome, err
}

func (r *runner) RebaseContinueNoEdit(ctx context.Context) (RebaseOutcome, error) {
	err := r.rebaseContinueOnce(ctx)
	if err != nil {
		if r.IsRebaseInProgress(ctx) {
			return r.continueRerereResolvedRebase(ctx, err)
		}
		return RebaseOutcome{Result: RebaseConflict}, err
	}
	if r.IsRebaseInProgress(ctx) {
		return r.continueRerereResolvedRebase(ctx, nil)
	}
	return RebaseOutcome{Result: RebaseDone}, nil
}

func (r *runner) RebaseContinue(ctx context.Context) (RebaseOutcome, error) {
	err := r.rebaseContinueOnce(ctx)
	if err != nil {
		if r.IsRebaseInProgress(ctx) {
			return r.continueRerereResolvedRebase(ctx, err)
		}
		return RebaseOutcome{Result: RebaseConflict}, fmt.Errorf("rebase continue failed: %w", err)
	}
	if r.IsRebaseInProgress(ctx) {
		return r.continueRerereResolvedRebase(ctx, nil)
	}

	return RebaseOutcome{Result: RebaseDone}, nil
}

func (r *runner) RebaseAbort(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "rebase", "--abort")
	r.revisionCache.InvalidateAll()
	if err != nil {
		return fmt.Errorf("rebase abort failed: %w", err)
	}
	return nil
}

func (r *runner) InteractiveRebase(_ context.Context, onto string) error {
	return r.RunGitCommandInteractive("rebase", "-i", onto)
}

func (r *runner) GetRebaseHead() (string, error) {
	// Try standard rebase head refs in order:
	// 1. REBASE_HEAD (standard)
	// 2. refs/rebase-merge/head (interactive)
	// 3. refs/rebase-apply/head (non-interactive)
	refs := []string{
		"REBASE_HEAD",
		"refs/rebase-merge/head",
		"refs/rebase-apply/head",
	}

	for _, refName := range refs {
		output, err := r.GetRef(refName)
		if err == nil && output != "" {
			return strings.TrimSpace(output), nil
		}
	}

	return "", fmt.Errorf("rebase head not found")
}

func (r *runner) IsRebaseInProgress(ctx context.Context) bool {
	gitDir := r.getGitDir(ctx)
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-merge")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-apply")); err == nil {
		return true
	}
	return false
}

func (r *runner) CheckRebaseInProgress(ctx context.Context) error {
	if r.IsRebaseInProgress(ctx) {
		return fmt.Errorf("a rebase is already in progress. Please finish or abort it first")
	}
	return nil
}
