package squash_test

import (
	"testing"

	"github.com/getstackit/stackit/internal/app/actiondeps"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/squash"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestSquashTargetBranchRestoresOriginal(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"feature": "main"}).
		Checkout("feature").
		CommitChange("one", "one").
		CommitChange("two", "two").
		Checkout("main")
	before := s.BranchCommitCount("feature")
	require.Greater(t, before, 1)

	err := squash.Action(t.Context(), deps(s), squash.Options{Branch: "feature", NoEdit: true})
	require.NoError(t, err)

	s.Rebuild().ExpectBranch("main")
	require.Equal(t, 1, s.BranchCommitCount("feature"))
}

func deps(s *scenario.Scenario) squash.Deps {
	return squash.Deps{
		Engine:       s.Context.Engine,
		MutationDeps: actiondeps.Mutation(s.Context),
		Log:          s.Context.Logger,
		Verify:       s.Context.Verify,
	}
}
