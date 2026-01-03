package engine

import (
	"context"
	"strings"

	"stackit.dev/stackit/internal/git"
)

// GetPendingChanges returns the status of pending changes in the working directory
func (e *engineImpl) GetPendingChanges(ctx context.Context) ([]PendingChange, error) {
	output, err := e.git.GetStatusPorcelain(ctx)
	if err != nil {
		return nil, err
	}

	var changes []PendingChange
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		// Porcelain format: XY path
		// X is staged status, Y is unstaged status
		x := line[0]
		y := line[1]
		path := strings.TrimSpace(line[3:])

		if x != ' ' && x != '?' {
			changes = append(changes, PendingChange{
				Path:   path,
				Status: string(x),
				Staged: true,
			})
		}
		if y != ' ' {
			status := string(y)
			if x == '?' && y == '?' {
				status = "??"
			}
			changes = append(changes, PendingChange{
				Path:   path,
				Status: status,
				Staged: false,
			})
		}
	}

	return changes, nil
}

// GetUnstagedDiff returns the unstaged diff
func (e *engineImpl) GetUnstagedDiff(ctx context.Context, files ...string) (string, error) {
	return e.git.GetUnstagedDiff(ctx, files...)
}

// HasStagedChanges checks if there are staged changes in the repository
func (e *engineImpl) HasStagedChanges(ctx context.Context) (bool, error) {
	return e.git.HasStagedChanges(ctx)
}

// HasUnstagedChanges checks if there are unstaged changes in the repository
func (e *engineImpl) HasUnstagedChanges(ctx context.Context) (bool, error) {
	return e.git.HasUnstagedChanges(ctx)
}

// GetMergeBase returns the merge base between two revisions
func (e *engineImpl) GetMergeBase(rev1, rev2 string) (string, error) {
	return e.git.GetMergeBase(rev1, rev2)
}

// IsDiffEmpty checks if the diff between base and head is empty
func (e *engineImpl) IsDiffEmpty(ctx context.Context, base, head string) (bool, error) {
	return e.git.IsDiffEmpty(ctx, base, head)
}

// GetChangedFiles returns the list of files changed between base and head
func (e *engineImpl) GetChangedFiles(ctx context.Context, base, head string) ([]string, error) {
	return e.git.GetChangedFiles(ctx, base, head)
}

// ListWorktrees returns a list of worktree paths
func (e *engineImpl) ListWorktrees(ctx context.Context) ([]string, error) {
	return e.git.ListWorktrees(ctx)
}

// GetRemoteURL returns the remote URL
func (e *engineImpl) GetRemoteURL(_ context.Context) (string, error) {
	return e.git.GetConfig("remote.origin.url")
}

// GetCurrentRevision returns the current revision (HEAD)
func (e *engineImpl) GetCurrentRevision(_ context.Context) (string, error) {
	return e.git.GetRevision("HEAD")
}

// GetReflog returns the reflog
func (e *engineImpl) GetReflog(ctx context.Context, count int, format string) (string, error) {
	return e.git.GetReflog(ctx, count, format)
}

// CheckoutPaths checks out specific paths from a branch
func (e *engineImpl) CheckoutPaths(ctx context.Context, branch string, pathspecs []string) error {
	return e.git.CheckoutPaths(ctx, branch, pathspecs)
}

// RemovePaths removes specific paths from the working tree
func (e *engineImpl) RemovePaths(ctx context.Context, pathspecs []string) error {
	return e.git.RemovePaths(ctx, pathspecs)
}

// StashList returns the stash list
func (e *engineImpl) StashList(ctx context.Context) (string, error) {
	return e.git.ListStash(ctx)
}

// ParseStagedHunks parses the output of `git diff --cached` into structured hunks
func (e *engineImpl) ParseStagedHunks(ctx context.Context) ([]git.Hunk, error) {
	return e.git.ParseStagedHunks(ctx)
}

// ShowDiff returns the diff between two refs with optional stat mode
func (e *engineImpl) ShowDiff(ctx context.Context, left, right string, stat bool) (string, error) {
	return e.git.ShowDiff(ctx, left, right, stat)
}

// ShowCommits returns commit log with optional patches/stat
func (e *engineImpl) ShowCommits(ctx context.Context, base, head string, patch, stat bool) (string, error) {
	return e.git.ShowCommits(ctx, base, head, patch, stat)
}

// GetCommitTemplate returns the commit template
func (e *engineImpl) GetCommitTemplate(ctx context.Context) (string, error) {
	return e.git.GetCommitTemplate(ctx)
}

// GetUnmergedFiles returns list of files with merge conflicts
func (e *engineImpl) GetUnmergedFiles(ctx context.Context) ([]string, error) {
	return e.git.GetUnmergedFiles(ctx)
}

// GetParentCommitSHA returns the parent commit SHA of a commit
func (e *engineImpl) GetParentCommitSHA(commitSHA string) (string, error) {
	return e.git.GetParentCommitSHA(commitSHA)
}

// GetCommitSHA returns the SHA at a relative position (0 = HEAD, 1 = HEAD~1)
func (e *engineImpl) GetCommitSHA(branchName string, offset int) (string, error) {
	return e.git.GetCommitSHA(branchName, offset)
}

// IsAncestor checks if one commit is an ancestor of another
func (e *engineImpl) IsAncestor(ancestor, descendant string) (bool, error) {
	return e.git.IsAncestor(ancestor, descendant)
}

// IsRebaseInProgress checks if a rebase is in progress
func (e *engineImpl) IsRebaseInProgress(ctx context.Context) bool {
	return e.git.IsRebaseInProgress(ctx)
}

// GetRebaseHead returns the current rebase head
func (e *engineImpl) GetRebaseHead() (string, error) {
	return e.git.GetRebaseHead()
}

// HasUncommittedChanges checks if there are uncommitted changes
func (e *engineImpl) HasUncommittedChanges(ctx context.Context) bool {
	return e.git.HasUncommittedChanges(ctx)
}

// GetRepoInfo returns the repository owner and name
func (e *engineImpl) GetRepoInfo(ctx context.Context) (string, string, error) {
	return e.git.GetRepoInfo(ctx)
}

// IsInsideRepo checks if the current directory is inside a git repository
func (e *engineImpl) IsInsideRepo() bool {
	return e.git.IsInsideRepo()
}
