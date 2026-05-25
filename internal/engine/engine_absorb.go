package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

// ApplyHunksToBranch applies multiple hunks to commits in a branch by recreating them.
func (e *engineImpl) ApplyHunksToBranch(ctx context.Context, branch Branch, hunksByCommit map[string][]git.Hunk) error {
	if len(hunksByCommit) == 0 {
		return nil
	}

	branchName := branch.GetName()

	// Save current state to restore later
	originalRef, _ := e.git.GetCurrentBranchOrSHA(ctx)
	originalRef = strings.TrimSpace(originalRef)

	// Cleanup function to restore state on error
	cleanup := func() {
		// Reset any unmerged/dirty state
		_ = e.git.HardReset(ctx, "HEAD")
		// Return to original ref
		if originalRef != "" {
			_ = e.git.CheckoutBranch(ctx, originalRef)
		}
	}

	defer func() {
		// Always try to return to original ref, but don't reset if successful
		if originalRef != "" {
			_ = e.git.CheckoutBranch(ctx, originalRef)
		}
	}()

	// Get all commits in the branch
	commitSHAs, err := branch.GetAllCommits(CommitFormatSHA)
	if err != nil {
		return fmt.Errorf("failed to get commits for branch %s: %w", branchName, err)
	}

	if len(commitSHAs) == 0 {
		return fmt.Errorf("branch %s has no commits", branchName)
	}

	// Get base revision (parent of the first commit in the branch)
	meta, err := e.readMetadata(branchName)
	if err != nil {
		return fmt.Errorf("failed to read metadata for %s: %w", branchName, err)
	}
	if meta.GetParentBranchRevision() == nil {
		return fmt.Errorf("branch %s has no parent revision", branchName)
	}
	currentBase := *meta.GetParentBranchRevision()

	// Checkout base in detached HEAD
	if err := e.git.CheckoutDetached(ctx, currentBase); err != nil {
		return fmt.Errorf("failed to checkout base %s: %w", currentBase[:8], err)
	}

	// Recreate branch commit by commit (oldest to newest)
	for i := len(commitSHAs) - 1; i >= 0; i-- {
		commitSHA := commitSHAs[i]
		hunks, hasHunks := hunksByCommit[commitSHA]

		// 1. Cherry-pick the original commit
		if err := e.git.CherryPickSimple(ctx, commitSHA); err != nil {
			_ = e.git.CherryPickAbort(ctx)
			cleanup()
			return fmt.Errorf("failed to cherry-pick %s: %w", commitSHA[:8], err)
		}

		if hasHunks {
			// 2. Apply hunks to this commit
			tmpDir, err := os.MkdirTemp("", fmt.Sprintf("stackit-absorb-%s-*", commitSHA[:8]))
			if err != nil {
				cleanup()
				return fmt.Errorf("failed to create temp directory: %w", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			patchFile := filepath.Join(tmpDir, "hunks.patch")
			var patchContent strings.Builder
			hunksByFile := make(map[string][]git.Hunk)
			for _, hunk := range hunks {
				hunksByFile[hunk.File] = append(hunksByFile[hunk.File], hunk)
			}
			for file, fileHunks := range hunksByFile {
				fmt.Fprintf(&patchContent, "diff --git a/%s b/%s\n", file, file)
				// Include index line if available (needed for --3way merge)
				if len(fileHunks) > 0 && fileHunks[0].IndexLine != "" {
					patchContent.WriteString(fileHunks[0].IndexLine + "\n")
				}
				fmt.Fprintf(&patchContent, "--- a/%s\n", file)
				fmt.Fprintf(&patchContent, "+++ b/%s\n", file)
				for _, hunk := range fileHunks {
					patchContent.WriteString(hunk.Content)
					if !strings.HasSuffix(hunk.Content, "\n") {
						patchContent.WriteString("\n")
					}
				}
			}
			if err := os.WriteFile(patchFile, []byte(patchContent.String()), 0600); err != nil {
				cleanup()
				return fmt.Errorf("failed to write hunks patch: %w", err)
			}

			// Apply hunks to the worktree and index using --3way for better conflict handling
			// --3way allows git to fall back to three-way merge when the patch context doesn't match exactly
			if err := e.git.ApplyPatch(ctx, patchFile, true); err != nil {
				cleanup()
				return fmt.Errorf("failed to apply hunks for commit %s: %w", commitSHA[:8], err)
			}

			// Check for merge conflicts - git apply --3way can succeed but leave conflicts
			unmerged, _ := e.git.GetUnmergedFiles(ctx)
			if len(unmerged) > 0 {
				// There are unmerged files - this means --3way resulted in conflicts
				conflictFiles := strings.Join(unmerged, "\n")
				cleanup()
				return fmt.Errorf("merge conflict while applying hunks for commit %s. Conflicting files:\n%s\n\nThe changes could not be automatically merged. Consider manually applying the changes or splitting them into separate commits", commitSHA[:8], conflictFiles)
			}

			// 3. Amend the commit
			if err := e.git.CommitAmendNoEdit(ctx); err != nil {
				cleanup()
				return fmt.Errorf("failed to amend commit %s: %w", commitSHA[:8], err)
			}
		}
	}

	// Get new tip
	newTip, err := e.git.GetCurrentRevision(ctx)
	if err != nil {
		return fmt.Errorf("failed to get new tip: %w", err)
	}
	newTip = strings.TrimSpace(newTip)

	// Update branch to point to new tip
	if err := e.git.UpdateBranchRef(ctx, branchName, newTip); err != nil {
		return fmt.Errorf("failed to update branch %s: %w", branchName, err)
	}

	return nil
}

// FindTargetCommitForHunk finds the first commit downstack where the hunk doesn't commute
func (e *engineImpl) FindTargetCommitForHunk(hunk git.Hunk, commitSHAs []string) (string, int, error) {
	if len(commitSHAs) == 0 {
		return "", -1, nil
	}

	// Iterate through commits from newest to oldest
	for i, commitSHA := range commitSHAs {
		// Get parent commit SHA
		parentSHA, err := e.git.GetParentCommitSHA(commitSHA)
		if err != nil {
			// If we can't get parent, skip this commit
			continue
		}

		// Check if hunk commutes with this commit
		commutes, err := e.git.CheckCommutation(hunk, commitSHA, parentSHA)
		if err != nil {
			return "", -1, fmt.Errorf("failed to check commutation: %w", err)
		}

		if !commutes {
			// Found the target commit - hunk doesn't commute with it
			return commitSHA, i, nil
		}
	}

	// Hunk commutes with all commits
	return "", -1, nil
}
