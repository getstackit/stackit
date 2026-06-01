package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

type fastPathGit struct {
	git.Runner

	t              *testing.T
	mergeTreeArgs  []string
	commitTreeEnv  []string
	commitTreeArgs []string
}

func (g *fastPathGit) GetCommitRangeSHAs(_ context.Context, base, head string) ([]string, error) {
	require.Equal(g.t, "old-base", base)
	require.Equal(g.t, "feature", head)
	return []string{"feature-commit"}, nil
}

func (g *fastPathGit) GetChangedFiles(_ context.Context, base, head string) ([]string, error) {
	require.Equal(g.t, "old-base", base)
	switch head {
	case "new-parent":
		return []string{"parent.txt"}, nil
	case "feature":
		return []string{"feature.txt"}, nil
	default:
		g.t.Fatalf("unexpected changed-files head: %s", head)
		return nil, nil
	}
}

func (g *fastPathGit) RunGitCommandWithContext(_ context.Context, args ...string) (string, error) {
	g.mergeTreeArgs = append([]string(nil), args...)
	return "tree-sha\n", nil
}

func (g *fastPathGit) GetCommitLog(sha, format string) (string, error) {
	require.Equal(g.t, "feature-commit", sha)
	switch format {
	case "%an":
		return "Author Name\n", nil
	case "%ae":
		return "author@example.com\n", nil
	case "%aI":
		return "2026-06-01T12:00:00-04:00\n", nil
	case "%B":
		return "subject\n\nbody\n", nil
	default:
		g.t.Fatalf("unexpected commit log format: %s", format)
		return "", nil
	}
}

func (g *fastPathGit) RunGitCommandWithEnv(_ context.Context, env []string, args ...string) (string, error) {
	g.commitTreeEnv = append([]string(nil), env...)
	g.commitTreeArgs = append([]string(nil), args...)
	return "new-sha\n", nil
}

func TestTryConflictFreeReplayUsesMergeTreeWithExplicitMergeBase(t *testing.T) {
	fakeGit := &fastPathGit{t: t}
	eng := &engineImpl{git: fakeGit}

	newSHA, ok := eng.tryConflictFreeReplay(context.Background(), RebaseSpec{
		Branch:      "feature",
		NewParent:   "new-parent",
		OldUpstream: "old-base",
	}, "new-parent")

	require.True(t, ok)
	require.Equal(t, "new-sha", newSHA)
	require.Equal(t, []string{
		"merge-tree",
		"--write-tree",
		"--merge-base",
		"old-base",
		"new-parent",
		"feature",
	}, fakeGit.mergeTreeArgs)
	require.Equal(t, []string{
		"GIT_AUTHOR_NAME=Author Name",
		"GIT_AUTHOR_EMAIL=author@example.com",
		"GIT_AUTHOR_DATE=2026-06-01T12:00:00-04:00",
	}, fakeGit.commitTreeEnv)
	require.Equal(t, []string{
		"commit-tree",
		"tree-sha",
		"-p",
		"new-parent",
		"-m",
		"subject\n\nbody",
	}, fakeGit.commitTreeArgs)
}
