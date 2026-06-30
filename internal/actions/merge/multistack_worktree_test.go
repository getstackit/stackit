package merge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestMultiStackWorktreeExecutor_ConflictingStackResetsState(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// stack1 modifies test.txt to "stack1"
	s.CreateBranchQuiet("stack1").
		TrackBranch("stack1", s.Engine.Trunk().GetName())
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("stack1", ""))
	s.Rebuild().
		CheckoutQuiet(s.Engine.Trunk().GetName())

	// stack2 modifies the same file differently to force a conflict when merged after stack1
	s.CreateBranchQuiet("stack2").
		TrackBranch("stack2", s.Engine.Trunk().GetName())
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("stack2", ""))
	s.Rebuild().
		Checkout(s.Engine.Trunk().GetName())

	executor := NewMultiStackWorktreeExecutor(s.Engine, s.Context.Output)
	result, err := executor.ExecuteInWorktree(context.Background(), []MultiStackInfo{
		{RootBranch: "stack1", AllBranches: []string{"stack1"}},
		{RootBranch: "stack2", AllBranches: []string{"stack2"}},
	})
	require.NoError(t, err)
	defer result.Cleanup()

	require.Len(t, result.MergedStacks, 1)
	assert.Equal(t, "stack1", result.MergedStacks[0].RootBranch)

	require.Len(t, result.ConflictStacks, 1)
	assert.Equal(t, "stack2", result.ConflictStacks[0].Stack.RootBranch)

	// Verify the worktree does not contain partial changes from the conflicting stack
	content, readErr := os.ReadFile(filepath.Join(result.WorktreePath, "test.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "stack1", string(content))
}

func TestMultiStackWorktreeExecutor_PullsTrunkBeforeMerge(t *testing.T) {
	t.Parallel()
	s := scenario.NewRemoteScenario(t)

	remotePath, err := s.Scene.Repo.RunGitCommandAndGetOutput("remote", "get-url", "origin")
	require.NoError(t, err)

	// Create a new commit on trunk and push it to the remote, then rewind local to make it stale
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("remote-change", "remote"))
	require.NoError(t, s.Scene.Repo.PushBranch("origin", s.Engine.Trunk().GetName()))

	latestRemoteSHA, err := s.Scene.Repo.GetRevision("origin/" + s.Engine.Trunk().GetName())
	require.NoError(t, err)

	// Reset local trunk to previous commit to simulate stale trunk
	prevSHA, err := s.Scene.Repo.GetRevision(s.Engine.Trunk().GetName() + "~1")
	require.NoError(t, err)
	require.NoError(t, s.Scene.Repo.RunGitCommand("reset", "--hard", prevSHA))
	s.Rebuild()

	executor := NewMultiStackWorktreeExecutor(s.Engine, s.Context.Output)
	result, err := executor.ExecuteInWorktree(context.Background(), nil)
	require.NoError(t, err)
	defer result.Cleanup()

	worktreeRepo := testhelpers.NewGitRepoFromExisting(t, result.WorktreePath)
	worktreeHead, err := worktreeRepo.GetRevision("HEAD")
	require.NoError(t, err)

	assert.Equal(t, latestRemoteSHA, worktreeHead)
	assert.Contains(t, remotePath, "origin")
}

func TestMultiStackWorktreeExecutor_OctopusMergeCreatesSingleCommit(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create a stack with 3 branches: branch1 -> branch2 -> branch3
	// Each branch modifies different files to avoid conflicts
	s.CreateBranchQuiet("branch1").
		TrackBranch("branch1", s.Engine.Trunk().GetName())
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("branch1-content", "file1"))
	branch1SHA, err := s.Scene.Repo.GetRevision("branch1")
	require.NoError(t, err)
	s.Rebuild()

	s.CreateBranchQuiet("branch2").
		TrackBranch("branch2", "branch1")
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("branch2-content", "file2"))
	branch2SHA, err := s.Scene.Repo.GetRevision("branch2")
	require.NoError(t, err)
	s.Rebuild()

	s.CreateBranchQuiet("branch3").
		TrackBranch("branch3", "branch2")
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("branch3-content", "file3"))
	branch3SHA, err := s.Scene.Repo.GetRevision("branch3")
	require.NoError(t, err)
	s.Rebuild().
		CheckoutQuiet(s.Engine.Trunk().GetName())

	// Execute octopus merge
	executor := NewMultiStackWorktreeExecutor(s.Engine, s.Context.Output)
	result, err := executor.ExecuteInWorktree(context.Background(), []MultiStackInfo{
		{RootBranch: "branch1", AllBranches: []string{"branch1", "branch2", "branch3"}},
	})
	require.NoError(t, err)
	defer result.Cleanup()

	require.Len(t, result.MergedStacks, 1)
	assert.Equal(t, "branch1", result.MergedStacks[0].RootBranch)

	// Verify we have a merge commit (Git optimizes stacked branches to 2 parents: trunk + top branch)
	worktreeRepo := testhelpers.NewGitRepoFromExisting(t, result.WorktreePath)

	// Get the commit object to count parents
	parentOutput, err := worktreeRepo.RunGitCommandAndGetOutput("cat-file", "-p", "HEAD")
	require.NoError(t, err)

	// Count "parent" lines in the commit object
	parentCount := 0
	for _, line := range splitLines(parentOutput) {
		if len(line) > 6 && line[:6] == "parent" {
			parentCount++
		}
	}

	// For stacked branches, Git optimizes to 2 parents (trunk + top branch)
	// since the top branch already contains all commits from lower branches
	assert.Equal(t, 2, parentCount, "merge should have 2 parents (trunk + top branch)")

	// Verify all original branch commits are ancestors of HEAD (this is the critical check)
	// GitHub will auto-close PRs because their commits are reachable from the merged commit
	isAncestor := func(sha string) bool {
		err := worktreeRepo.RunGitCommand("merge-base", "--is-ancestor", sha, "HEAD")
		return err == nil
	}

	assert.True(t, isAncestor(branch1SHA), "branch1 commit should be ancestor of HEAD")
	assert.True(t, isAncestor(branch2SHA), "branch2 commit should be ancestor of HEAD")
	assert.True(t, isAncestor(branch3SHA), "branch3 commit should be ancestor of HEAD")

	// Verify all files are present in the working tree
	// CreateChangeAndCommit creates files as "{prefix}_test.txt"
	_, err = os.Stat(filepath.Join(result.WorktreePath, "file1_test.txt"))
	assert.NoError(t, err, "file1_test.txt should exist")
	_, err = os.Stat(filepath.Join(result.WorktreePath, "file2_test.txt"))
	assert.NoError(t, err, "file2_test.txt should exist")
	_, err = os.Stat(filepath.Join(result.WorktreePath, "file3_test.txt"))
	assert.NoError(t, err, "file3_test.txt should exist")
}

func TestMultiStackWorktreeExecutor_GlobalOctopusMergeAcrossStacks(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create stack1 with 2 branches modifying different files
	s.CreateBranchQuiet("stack1-branch1").
		TrackBranch("stack1-branch1", s.Engine.Trunk().GetName())
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("stack1-b1", "s1file1"))
	stack1Branch1SHA, err := s.Scene.Repo.GetRevision("stack1-branch1")
	require.NoError(t, err)
	s.Rebuild()

	s.CreateBranchQuiet("stack1-branch2").
		TrackBranch("stack1-branch2", "stack1-branch1")
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("stack1-b2", "s1file2"))
	stack1Branch2SHA, err := s.Scene.Repo.GetRevision("stack1-branch2")
	require.NoError(t, err)
	s.Rebuild().
		CheckoutQuiet(s.Engine.Trunk().GetName())

	// Create stack2 with 2 branches modifying different files (no conflicts with stack1)
	s.CreateBranchQuiet("stack2-branch1").
		TrackBranch("stack2-branch1", s.Engine.Trunk().GetName())
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("stack2-b1", "s2file1"))
	stack2Branch1SHA, err := s.Scene.Repo.GetRevision("stack2-branch1")
	require.NoError(t, err)
	s.Rebuild()

	s.CreateBranchQuiet("stack2-branch2").
		TrackBranch("stack2-branch2", "stack2-branch1")
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("stack2-b2", "s2file2"))
	stack2Branch2SHA, err := s.Scene.Repo.GetRevision("stack2-branch2")
	require.NoError(t, err)
	s.Rebuild().
		CheckoutQuiet(s.Engine.Trunk().GetName())

	// Execute multi-stack merge
	executor := NewMultiStackWorktreeExecutor(s.Engine, s.Context.Output)
	result, err := executor.ExecuteInWorktree(context.Background(), []MultiStackInfo{
		{RootBranch: "stack1-branch1", AllBranches: []string{"stack1-branch1", "stack1-branch2"}},
		{RootBranch: "stack2-branch1", AllBranches: []string{"stack2-branch1", "stack2-branch2"}},
	})
	require.NoError(t, err)
	defer result.Cleanup()

	// Both stacks should be merged
	require.Len(t, result.MergedStacks, 2)
	assert.Empty(t, result.ConflictStacks)

	worktreeRepo := testhelpers.NewGitRepoFromExisting(t, result.WorktreePath)

	// Verify we have exactly ONE merge commit (global octopus) by counting first-parent commits
	// First-parent traversal shows just the merge commits on the mainline
	logOutput, err := worktreeRepo.RunGitCommandAndGetOutput("rev-list", "--count", "--first-parent", s.Engine.Trunk().GetName()+"..HEAD")
	require.NoError(t, err)
	assert.Equal(t, "1", logOutput, "should have exactly 1 merge commit on first-parent path (global octopus)")

	// Verify HEAD has multiple parents (octopus merge)
	parentOutput, err := worktreeRepo.RunGitCommandAndGetOutput("cat-file", "-p", "HEAD")
	require.NoError(t, err)
	parentCount := 0
	for _, line := range splitLines(parentOutput) {
		if len(line) > 6 && line[:6] == "parent" {
			parentCount++
		}
	}
	// Git optimizes stacked branches, so we expect at least 3 parents:
	// trunk + top of stack1 + top of stack2
	assert.GreaterOrEqual(t, parentCount, 3, "octopus merge should have at least 3 parents")

	// Verify all branch commits are ancestors
	isAncestor := func(sha string) bool {
		err := worktreeRepo.RunGitCommand("merge-base", "--is-ancestor", sha, "HEAD")
		return err == nil
	}

	assert.True(t, isAncestor(stack1Branch1SHA), "stack1-branch1 should be ancestor of HEAD")
	assert.True(t, isAncestor(stack1Branch2SHA), "stack1-branch2 should be ancestor of HEAD")
	assert.True(t, isAncestor(stack2Branch1SHA), "stack2-branch1 should be ancestor of HEAD")
	assert.True(t, isAncestor(stack2Branch2SHA), "stack2-branch2 should be ancestor of HEAD")

	// Verify all files are present
	_, err = os.Stat(filepath.Join(result.WorktreePath, "s1file1_test.txt"))
	assert.NoError(t, err, "s1file1_test.txt should exist")
	_, err = os.Stat(filepath.Join(result.WorktreePath, "s1file2_test.txt"))
	assert.NoError(t, err, "s1file2_test.txt should exist")
	_, err = os.Stat(filepath.Join(result.WorktreePath, "s2file1_test.txt"))
	assert.NoError(t, err, "s2file1_test.txt should exist")
	_, err = os.Stat(filepath.Join(result.WorktreePath, "s2file2_test.txt"))
	assert.NoError(t, err, "s2file2_test.txt should exist")
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
