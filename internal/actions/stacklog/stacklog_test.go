package stacklog_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/stacklog"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func branchNames(res stacklog.Result) []string {
	names := make([]string, len(res.Branches))
	for i, b := range res.Branches {
		names[i] = b.Name
	}
	return names
}

func TestGather_AncestorPathTopDown(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
	s.Checkout("c")

	res, err := stacklog.Gather(s.Engine)
	require.NoError(t, err)

	require.False(t, res.OnTrunk)
	require.Equal(t, "main", res.TrunkName)
	require.NotEmpty(t, res.TrunkTipSHA)

	// Current branch first, trunk-most ancestor last; trunk excluded.
	require.Equal(t, []string{"c", "b", "a"}, branchNames(res))

	require.True(t, res.Branches[0].IsCurrent, "c should be marked current")
	require.False(t, res.Branches[1].IsCurrent)

	// Each branch carries its own commit with a full SHA and readable subject.
	for _, b := range res.Branches {
		require.NotEmpty(t, b.Commits, "branch %s should have commits", b.Name)
		require.Len(t, b.Commits[0].SHA, 40, "expected full SHA for %s", b.Name)
		require.Contains(t, b.Commits[0].Subject, "change on "+b.Name)
	}
}

func TestGather_FromMiddleOfStackExcludesChildren(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
	s.Checkout("b")

	res, err := stacklog.Gather(s.Engine)
	require.NoError(t, err)

	// Standing on b: only b and its ancestor a; child c is out of scope.
	require.Equal(t, []string{"b", "a"}, branchNames(res))
	require.True(t, res.Branches[0].IsCurrent)
}

func TestGather_OnTrunkHasNoStackBand(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
	s.Checkout("main")

	res, err := stacklog.Gather(s.Engine)
	require.NoError(t, err)

	require.True(t, res.OnTrunk)
	require.Empty(t, res.Branches)
	require.Equal(t, "main", res.TrunkName)
}

func TestGather_DecorationsIncludeBranchesAndTags(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
	s.Checkout("c")
	require.NoError(t, s.Scene.Repo.RunGitCommand("tag", "-a", "v1.0.0", "-m", "release"))

	res, err := stacklog.Gather(s.Engine)
	require.NoError(t, err)

	// The tag on c's tip resolves to c's tip commit, alongside the branch head.
	tipSHA := res.Branches[0].Commits[0].SHA
	decos := res.Decorations[tipSHA]
	require.NotEmpty(t, decos)

	var sawBranch, sawTag bool
	for _, d := range decos {
		switch {
		case d.IsTag && d.Name == "v1.0.0":
			sawTag = true
		case !d.IsTag && d.Name == "c":
			sawBranch = true
		}
	}
	require.True(t, sawBranch, "expected branch head 'c' decoration: %+v", decos)
	require.True(t, sawTag, "expected annotated tag 'v1.0.0' decoration: %+v", decos)
}
