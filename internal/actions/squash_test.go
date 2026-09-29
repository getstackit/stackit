package actions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestSquashActionTargetBranchRestoresOriginal(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"feature": "main"}).
		Checkout("feature").
		CommitChange("one", "one").
		CommitChange("two", "two").
		Checkout("main")
	before := s.BranchCommitCount("feature")
	require.Greater(t, before, 1)

	err := SquashAction(s.Context, SquashOptions{Branch: "feature", NoEdit: true})
	require.NoError(t, err)

	s.Rebuild().ExpectBranch("main")
	require.Equal(t, 1, s.BranchCommitCount("feature"))
}
