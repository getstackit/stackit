package sync

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestCleanBranchesNonInteractiveRespectsDirtyStackFilter(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"branch1": "main",
		})

	// Mark branch as deletable.
	s.Checkout("main").RunGit("merge", "branch1")
	err := s.Engine.Rebuild("main")
	require.NoError(t, err)
	err = s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("branch1"), testhelpers.NewTestPrInfoMerged(1, "main"))
	require.NoError(t, err)

	// Non-interactive path uses NullHandler. Dirty stack filter should prevent deletion.
	result, err := cleanBranches(
		s.Context,
		&Options{Force: true},
		map[string]bool{"branch1": true},
		&NullHandler{},
		&Summary{},
	)
	require.NoError(t, err)
	require.Empty(t, result.DeletedBranches)
	require.True(t, s.Engine.GetBranch("branch1").IsTracked())
}

type interactiveDeleteTestHandler struct {
	NullHandler
	promptedBranches map[string]string
	promptedUnpushed map[string]bool
}

func (h *interactiveDeleteTestHandler) IsInteractive() bool { return true }

func (h *interactiveDeleteTestHandler) PromptBranchDeletions(branches map[string]string, unpushedBranches map[string]bool) (map[string]bool, error) {
	h.promptedBranches = make(map[string]string, len(branches))
	maps.Copy(h.promptedBranches, branches)
	h.promptedUnpushed = make(map[string]bool, len(unpushedBranches))
	maps.Copy(h.promptedUnpushed, unpushedBranches)
	// Simulate user choosing not to delete any prompted branches.
	return map[string]bool{}, nil
}

func TestCleanBranchesInteractiveDoesNotAutoDeleteUnpushedUtilityBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"util-branch": "main",
		})

	// Set up remote and push trunk + branch so ahead/diverged status can be computed.
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)
	err = s.Scene.Repo.PushBranch("origin", "main")
	require.NoError(t, err)
	err = s.Scene.Repo.PushBranch("origin", "util-branch")
	require.NoError(t, err)

	// Add an unpushed local commit.
	s.Checkout("util-branch").
		CommitChange("unpushed.txt", "local-only commit")

	// Mark branch as utility and deletable (merged PR).
	err = s.Engine.SetBranchType(s.Engine.GetBranch("util-branch"), git.BranchTypeUtility)
	require.NoError(t, err)
	err = s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("util-branch"), testhelpers.NewTestPrInfoMerged(1, "main"))
	require.NoError(t, err)
	err = s.Engine.PopulateRemoteShas()
	require.NoError(t, err)

	s.Checkout("main")

	handler := &interactiveDeleteTestHandler{}
	result, err := cleanBranches(
		s.Context,
		&Options{Force: true},
		nil,
		handler,
		&Summary{},
	)
	require.NoError(t, err)
	require.Empty(t, result.DeletedBranches)
	require.Contains(t, handler.promptedBranches, "util-branch")
	require.True(t, handler.promptedUnpushed["util-branch"])
	require.True(t, s.Engine.GetBranch("util-branch").IsTracked())
}
