package absorb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestPrintAbsorbPreviewMultipleCommitsSameBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"branch-a": "main",
		})

	s.Checkout("branch-a")
	s.Scene.Repo.CreateChangeAndCommit("first commit", "file-a")
	s.Scene.Repo.CreateChangeAndCommit("second commit", "file-b")

	commits, err := s.Engine.GetBranch("branch-a").GetAllCommits(engine.CommitFormatSHA)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(commits), 2)
	// GetAllCommits returns newest-first; "first commit" was made before
	// "second commit".
	secondSHA, firstSHA := commits[0], commits[1]

	hunksByCommit := map[string][]git.Hunk{
		firstSHA: {
			{File: "file-a", NewStart: 1, NewCount: 1, Content: "+a"},
		},
		secondSHA: {
			{File: "file-b", NewStart: 1, NewCount: 1, Content: "+b"},
		},
	}

	out := output.NewTestOutput()
	printAbsorbPlan(hunksByCommit, nil, s.Engine, out)

	printed := out.String()
	require.Contains(t, printed, "first commit")
	require.Contains(t, printed, "second commit")
}
