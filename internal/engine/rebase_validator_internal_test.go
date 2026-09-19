package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

// fastPathGit is a fake git.Runner that records the merge-tree / commit-tree
// calls tryConflictFreeReplay makes, so we can assert the exact replay sequence
// without a real repository.
type fastPathGit struct {
	git.Runner

	t *testing.T

	// commits returned by ReadCommitRanges (newest-first).
	commits []string
	// parent lists keyed by commit SHA, as returned by git log --format=%P.
	parents map[string]string
	// changed files keyed by the diff head ("new-parent" / branch name).
	changedFiles map[string][]string

	// recorded calls, in order.
	mergeTreeCalls  [][]string
	commitTreeCalls [][]string

	// monotonic counter so each synthesized commit SHA is unique.
	commitTreeN int
}

func (g *fastPathGit) ReadCommitRanges(_ context.Context, mode git.CommitReadMode, ranges ...git.RevRange) git.ReadResults[[]git.CommitMetadata] {
	require.Equal(g.t, git.CommitDetails, mode)
	require.Len(g.t, ranges, 1)
	rr := ranges[0]
	require.Equal(g.t, "old-base", rr.Base)
	require.Equal(g.t, "feature", rr.Head)
	commits := make([]git.CommitMetadata, 0, len(g.commits))
	for _, sha := range g.commits {
		commits = append(commits, git.CommitMetadata{
			SHA: sha, Parents: strings.Fields(g.parents[sha]),
			AuthorName: "Author " + sha, AuthorEmail: sha + "@example.com",
			AuthorDate: "2026-06-01T12:00:00-04:00", Message: "subject " + sha + "\n",
		})
	}
	return git.ReadResults[[]git.CommitMetadata]{Values: map[string][]git.CommitMetadata{rr.String(): commits}}
}

func (g *fastPathGit) ReadDiffs(_ context.Context, mode git.DiffReadMode, ranges ...git.RevRange) git.ReadResults[git.DiffSummary] {
	require.Equal(g.t, git.DiffNames, mode)
	require.Len(g.t, ranges, 2, "parent and branch file sets must be read together")
	result := git.ReadResults[git.DiffSummary]{Values: make(map[string]git.DiffSummary)}
	for _, rr := range ranges {
		require.Equal(g.t, "old-base", rr.Base)
		files, ok := g.changedFiles[rr.Head]
		if !ok {
			g.t.Fatalf("unexpected changed-files head: %s", rr.Head)
		}
		result.Values[rr.String()] = git.DiffSummary{Files: files}
	}
	return result
}

func (g *fastPathGit) RunGitCommandWithContext(_ context.Context, args ...string) (string, error) {
	g.mergeTreeCalls = append(g.mergeTreeCalls, append([]string(nil), args...))
	// The tree SHA is derived from the commit being replayed (the last arg) so
	// assertions can tie a tree back to its source commit.
	return "tree-of-" + args[len(args)-1] + "\n", nil
}

func (g *fastPathGit) RunGitCommandWithEnv(_ context.Context, env []string, args ...string) (string, error) {
	g.commitTreeCalls = append(g.commitTreeCalls, append([]string(nil), env...))
	g.commitTreeCalls = append(g.commitTreeCalls, append([]string(nil), args...))
	g.commitTreeN++
	return fmt.Sprintf("new-sha-%d\n", g.commitTreeN), nil
}

func TestTryConflictFreeReplaySingleCommitUsesMergeTreeWithExplicitMergeBase(t *testing.T) {
	fakeGit := &fastPathGit{
		t:       t,
		commits: []string{"feature-commit"},
		parents: map[string]string{
			"feature-commit": "old-base",
		},
		changedFiles: map[string][]string{
			"new-parent": {"parent.txt"},
			"feature":    {"feature.txt"},
		},
	}
	eng := &engineImpl{git: fakeGit}

	newSHA, ok := eng.tryConflictFreeReplay(context.Background(), RebaseSpec{
		Branch:      "feature",
		NewParent:   "new-parent",
		OldUpstream: "old-base",
	}, "new-parent")

	require.True(t, ok)
	require.Equal(t, "new-sha-1", newSHA)
	// Single commit: merge base is OldUpstream, ours is the new parent, theirs is
	// the commit SHA itself (not the branch name).
	require.Equal(t, [][]string{{
		"merge-tree", "--write-tree", "--merge-base",
		"old-base", "new-parent", "feature-commit",
	}}, fakeGit.mergeTreeCalls)
	require.Equal(t, []string{
		"GIT_AUTHOR_NAME=Author feature-commit",
		"GIT_AUTHOR_EMAIL=feature-commit@example.com",
		"GIT_AUTHOR_DATE=2026-06-01T12:00:00-04:00",
	}, fakeGit.commitTreeCalls[0])
	require.Equal(t, []string{
		"commit-tree", "tree-of-feature-commit", "-p", "new-parent", "-m", "subject feature-commit",
	}, fakeGit.commitTreeCalls[1])
}

func TestTryConflictFreeReplayUsesFastPathWhenParentChangedNoFiles(t *testing.T) {
	// Parent advanced without touching any files (same tree as old-base). An empty
	// parent file set cannot overlap branch changes, so the fast path should still
	// replay instead of falling back to a worktree dry-run.
	fakeGit := &fastPathGit{
		t:       t,
		commits: []string{"feature-commit"},
		parents: map[string]string{
			"feature-commit": "old-base",
		},
		changedFiles: map[string][]string{
			"new-parent": {},
			"feature":    {"feature.txt"},
		},
	}
	eng := &engineImpl{git: fakeGit}

	newSHA, ok := eng.tryConflictFreeReplay(context.Background(), RebaseSpec{
		Branch:      "feature",
		NewParent:   "new-parent",
		OldUpstream: "old-base",
	}, "new-parent")

	require.True(t, ok)
	require.Equal(t, "new-sha-1", newSHA)
	require.Len(t, fakeGit.mergeTreeCalls, 1)
}

func TestTryConflictFreeReplayMultiCommitChainsEachCommit(t *testing.T) {
	// Branch has three commits (newest-first): c3 -> c2 -> c1 -> old-base.
	fakeGit := &fastPathGit{
		t:       t,
		commits: []string{"c3", "c2", "c1"},
		parents: map[string]string{
			"c1": "old-base",
			"c2": "c1",
			"c3": "c2",
		},
		changedFiles: map[string][]string{
			"new-parent": {"parent.txt"},
			"feature":    {"a.txt", "b.txt", "c.txt"},
		},
	}
	eng := &engineImpl{git: fakeGit}

	newSHA, ok := eng.tryConflictFreeReplay(context.Background(), RebaseSpec{
		Branch:      "feature",
		NewParent:   "new-parent",
		OldUpstream: "old-base",
	}, "new-parent")

	require.True(t, ok)
	// The returned SHA is the rebased tip (third commit-tree result).
	require.Equal(t, "new-sha-3", newSHA)

	// Each commit is replayed oldest-first, with its original parent as the merge
	// base and the previous rebased result as the new base.
	require.Equal(t, [][]string{
		{"merge-tree", "--write-tree", "--merge-base", "old-base", "new-parent", "c1"},
		{"merge-tree", "--write-tree", "--merge-base", "c1", "new-sha-1", "c2"},
		{"merge-tree", "--write-tree", "--merge-base", "c2", "new-sha-2", "c3"},
	}, fakeGit.mergeTreeCalls)

	// commit-tree parents chain: c1 onto new-parent, c2 onto new-sha-1, c3 onto new-sha-2.
	require.Equal(t, []string{
		"commit-tree", "tree-of-c1", "-p", "new-parent", "-m", "subject c1",
	}, fakeGit.commitTreeCalls[1])
	require.Equal(t, []string{
		"commit-tree", "tree-of-c2", "-p", "new-sha-1", "-m", "subject c2",
	}, fakeGit.commitTreeCalls[3])
	require.Equal(t, []string{
		"commit-tree", "tree-of-c3", "-p", "new-sha-2", "-m", "subject c3",
	}, fakeGit.commitTreeCalls[5])
}
