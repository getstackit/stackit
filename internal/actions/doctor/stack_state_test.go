package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestCheckEmptyBranches(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		CreateBranch("withchanges").
		CommitChange("file1", "real change").
		Checkout("main").
		CreateBranch("nochanges").
		Checkout("main")

	empty := checkEmptyBranches(s.Engine)

	require.ElementsMatch(t, []string{"nochanges"}, empty)
}

func TestCheckStackStatePrunesOrphanedMetadataInOneBatch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"orphan1": "main",
			"orphan2": "main",
		}).
		Checkout("main").
		RunGit("branch", "-D", "orphan1").
		RunGit("branch", "-D", "orphan2")

	h := &recordingHandler{}
	warnings, errors := checkStackState(context.Background(), s.Engine, h, 0, 0, true)

	require.Equal(t, 0, errors)
	require.Equal(t, 0, warnings)

	var messages []string
	for _, c := range h.checks {
		messages = append(messages, c.message)
	}
	require.Contains(t, messages, "Pruned orphaned metadata for deleted branch orphan1")
	require.Contains(t, messages, "Pruned orphaned metadata for deleted branch orphan2")
	require.Contains(t, messages, "All 2 orphaned metadata ref(s) pruned")

	refs, err := s.Engine.ListMetadataRefs()
	require.NoError(t, err)
	require.NotContains(t, refs, "orphan1")
	require.NotContains(t, refs, "orphan2")
}
