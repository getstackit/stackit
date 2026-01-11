package engine

import (
	"context"
	"fmt"

	"stackit.dev/stackit/internal/git"
)

// RebaseSpec describes a planned rebase operation
type RebaseSpec struct {
	Branch      string // Branch to rebase
	NewParent   string // New upstream to rebase onto
	OldUpstream string // Current base to replay commits from
}

// RebaseValidation is the result of dry-run validation
type RebaseValidation struct {
	Success      bool              // Whether all rebases would succeed
	FailedBranch string            // Which branch caused the conflict (if any)
	ErrorMessage string            // Error message describing the failure
	NewSHAs      map[string]string // Branch -> resulting SHA after rebase (if successful)
}

// ValidateRebases tests if a sequence of rebases will succeed by performing them
// in an isolated temporary worktree. This allows checking for conflicts before
// modifying any state in the main repository.
//
// IMPORTANT: This uses dry-run rebases that do NOT update branch refs, keeping
// the main repository completely unmodified.
//
// Returns a RebaseValidation indicating success or the first failure encountered.
// The worktree is cleaned up automatically regardless of outcome.
func (e *engineImpl) ValidateRebases(ctx context.Context, specs []RebaseSpec) (*RebaseValidation, error) {
	if len(specs) == 0 {
		return &RebaseValidation{Success: true, NewSHAs: map[string]string{}}, nil
	}

	// Create temporary worktree for validation
	worktreePath, cleanup, err := e.CreateTemporaryWorktree(ctx, "HEAD", "stackit-validate-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create validation worktree: %w", err)
	}
	defer cleanup()

	// Create a git runner for the worktree
	wtGit := git.NewRunnerWithPath(worktreePath, nil)

	result := &RebaseValidation{
		Success: true,
		NewSHAs: make(map[string]string),
	}

	// Track rebased SHAs so subsequent rebases can use them.
	// We track both branch name -> new SHA and old SHA -> new SHA mappings
	// because callers may pass either branch names or resolved SHAs in NewParent.
	rebasedByName := make(map[string]string) // branch name -> new SHA
	rebasedBySHA := make(map[string]string)  // old SHA -> new SHA

	// Execute each rebase in sequence using dry-run mode
	for _, spec := range specs {
		// Check if NewParent refers to a branch we already rebased.
		// This handles chained rebases where child branches depend on their parent's new position.
		newParent := spec.NewParent

		// First check by branch name (if caller passed a name)
		if rebased, ok := rebasedByName[spec.NewParent]; ok {
			newParent = rebased
		} else if rebased, ok := rebasedBySHA[spec.NewParent]; ok {
			// Then check by old SHA (if caller resolved to SHA before calling)
			newParent = rebased
		}

		// Get the branch's current SHA before rebasing (to track old SHA -> new SHA)
		oldBranchSHA, _ := wtGit.GetRevision(spec.Branch)

		rebaseResult, newSHA, err := dryRunRebase(ctx, wtGit, spec.Branch, newParent, spec.OldUpstream)
		if err != nil || rebaseResult == git.RebaseConflict {
			result.Success = false
			result.FailedBranch = spec.Branch
			if err != nil {
				result.ErrorMessage = fmt.Sprintf("rebase failed: %v", err)
			} else {
				result.ErrorMessage = fmt.Sprintf("conflict rebasing onto %s", spec.NewParent)
			}

			// Abort the in-progress rebase if any
			if wtGit.IsRebaseInProgress(ctx) {
				_ = wtGit.RebaseAbort(ctx)
			}

			return result, nil
		}

		// Record the resulting SHA for both our result and for subsequent rebases
		result.NewSHAs[spec.Branch] = newSHA
		rebasedByName[spec.Branch] = newSHA
		if oldBranchSHA != "" {
			rebasedBySHA[oldBranchSHA] = newSHA
		}
	}

	return result, nil
}

// dryRunRebase performs a rebase without updating branch refs.
// This allows testing if a rebase would succeed without modifying the repository.
// Returns the rebase result, the new SHA (if successful), and any error.
func dryRunRebase(ctx context.Context, g git.Runner, branchName, upstream, oldUpstream string) (git.RebaseResult, string, error) {
	// Perform rebase in detached HEAD mode using branchName~0.
	// The ~0 suffix resolves to the same commit as branchName but tells git to check out
	// the commit directly (detached HEAD) rather than the branch ref. This means the rebase
	// results stay on the detached HEAD and the actual branch ref remains unchanged,
	// keeping the main repository unmodified during validation.
	_, err := g.RunGitCommandWithContext(ctx, "rebase", "--onto", upstream, oldUpstream, branchName+"~0")
	if err != nil {
		if g.IsRebaseInProgress(ctx) {
			return git.RebaseConflict, "", nil
		}
		// Abort rebase if it failed for other reasons
		_, _ = g.RunGitCommandWithContext(ctx, "rebase", "--abort")
		return git.RebaseConflict, "", err
	}

	// Get the resulting SHA from the detached HEAD
	newSHA, err := g.GetCurrentRevision(ctx)
	if err != nil {
		return git.RebaseConflict, "", fmt.Errorf("failed to get revision after rebase: %w", err)
	}

	// DO NOT update the branch ref - this is the key difference from normal Rebase
	// The branch ref stays unchanged, keeping the main repo unmodified

	return git.RebaseDone, newSHA, nil
}
