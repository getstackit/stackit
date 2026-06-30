package engine

import (
	"context"
	"fmt"
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

	// commits returned by GetCommitRangeSHAs (newest-first).
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

func (g *fastPathGit) GetCommitRangeSHAs(_ context.Context, base, head string) ([]string, error) {
	require.Equal(g.t, "old-base", base)
	require.Equal(g.t, "feature", head)
	return g.commits, nil
}

func (g *fastPathGit) GetChangedFiles(_ context.Context, base, head string) ([]string, error) {
	require.Equal(g.t, "old-base", base)
	files, ok := g.changedFiles[head]
	if !ok {
		g.t.Fatalf("unexpected changed-files head: %s", head)
	}
	return files, nil
}

func (g *fastPathGit) RunGitCommandWithContext(_ context.Context, args ...string) (string, error) {
	g.mergeTreeCalls = append(g.mergeTreeCalls, append([]string(nil), args...))
	// The tree SHA is derived from the commit being replayed (the last arg) so
	// assertions can tie a tree back to its source commit.
	return "tree-of-" + args[len(args)-1] + "\n", nil
}

func (g *fastPathGit) GetCommitLog(sha, format string) (string, error) {
	switch format {
	case "%P":
		parent, ok := g.parents[sha]
		if !ok {
			g.t.Fatalf("unexpected parent lookup for commit: %s", sha)
		}
		return parent + "\n", nil
	case "%an":
		return "Author " + sha + "\n", nil
	case "%ae":
		return sha + "@example.com\n", nil
	case "%aI":
		return "2026-06-01T12:00:00-04:00\n", nil
	case "%B":
		return "subject " + sha + "\n", nil
	default:
		g.t.Fatalf("unexpected commit log format: %s", format)
		return "", nil
	}
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
