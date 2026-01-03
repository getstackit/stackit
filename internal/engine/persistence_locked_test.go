package engine_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestPrInfoLockedPersistence(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit().
		CreateBranch("feature").
		Commit("f1").
		TrackBranch("feature", "main")

	eng := s.Engine
	branch := eng.GetBranch("feature")

	// 1. Lock the branch
	_, err := eng.SetLocked([]engine.Branch{branch}, engine.LockReasonUser)
	require.NoError(t, err)
	require.True(t, branch.IsLocked())
	require.Equal(t, string(engine.LockReasonUser), string(branch.GetLockReason()))

	// 2. Upsert PR info with lockReason="user"
	prNumber := 123
	prInfo := engine.NewPrInfo(&prNumber, "Title", "Body", "OPEN", "main", "http://url", false).WithLockReason(engine.LockReasonUser)
	err = eng.UpsertPrInfo(branch, prInfo)
	require.NoError(t, err)

	// Simulate push by updating remote SHA
	localSha, _ := eng.GetRevision(branch)
	// We need a way to set remote SHA. Since we're using a real git repo in the scenario,
	// we should probably just use PopulateRemoteShas after a real push or mock it.
	// For this test, let's just mock the behavior by setting the remote ref.
	s.RunGit("update-ref", "refs/remotes/origin/feature", localSha)
	err = eng.PopulateRemoteShas()
	require.NoError(t, err)

	// 3. Verify it's saved in the PR info
	gotPrInfo, err := branch.GetPrInfo()
	require.NoError(t, err)
	require.NotNil(t, gotPrInfo)
	require.True(t, gotPrInfo.IsLocked())

	// 4. Rebuild engine to simulate fresh state
	err = eng.Rebuild("main")
	require.NoError(t, err)

	branch = eng.GetBranch("feature")
	require.True(t, branch.IsLocked())

	gotPrInfo, err = branch.GetPrInfo()
	require.NoError(t, err)
	require.NotNil(t, gotPrInfo)
	require.True(t, gotPrInfo.IsLocked(), "Locked status should be persisted in PR info")

	// 5. Check submission status - should NOT need update if nothing changed
	status, err := branch.GetPRSubmissionStatus()
	require.NoError(t, err)
	if status.NeedsUpdate {
		t.Logf("Needs update: %v, Reason: %s", status.NeedsUpdate, status.Reason)
		// Check components of needsUpdate
		prInfo, _ := branch.GetPrInfo()
		parentBranch := eng.Trunk().GetName()
		baseChanged := prInfo.Base() != parentBranch
		remoteStatus, _ := eng.GetBranchRemoteStatus(branch.GetName())
		branchChanged := !remoteStatus.Matches()
		t.Logf("baseChanged: %v, branchChanged: %v, prInfo.IsLocked: %v, branch.IsLocked: %v", baseChanged, branchChanged, prInfo.IsLocked(), branch.IsLocked())
	}
	require.False(t, status.NeedsUpdate, "Should not need update if lock status is consistent")
}
