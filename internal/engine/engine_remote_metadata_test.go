package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestRemoteMetadataSync(t *testing.T) {
	t.Parallel()
	t.Run("detects and resolves metadata conflicts", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		sh.CreateBranch("feature-a").
			CommitChange("file-a", "content-a").
			TrackBranch("feature-a", "main")

		eng := sh.Engine

		branch := eng.GetBranch("feature-a")
		_, err := eng.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonNone)
		require.NoError(t, err)
		require.NoError(t, eng.SetScope(context.Background(), branch, engine.NewScope("local-scope")))

		require.False(t, eng.IsLocked(branch))
		require.Equal(t, "local-scope", eng.GetScope(branch).String())

		remoteMeta := git.NewMetaFrom(git.MetaFields{
			LockReason: git.LockReasonUser,
			Scope:      new("remote-scope"),
			LastModifiedBy: &git.ModifiedBy{
				GitName:  "Remote User",
				GitEmail: "remote@example.com",
			},
		})
		createRemoteMetadataRef(t, sh, "feature-a", remoteMeta)

		err = eng.LoadRemoteMetadataCache()
		require.NoError(t, err)

		diff, err := eng.ComputeMetadataDiff("feature-a")
		require.NoError(t, err)
		require.NotNil(t, diff, "expected diff to be non-nil")
		require.True(t, diff.HasConflict, "expected conflict to be detected")
		require.Len(t, diff.Differences, 2, "expected 2 field differences (locked and scope)")

		fieldNames := make(map[string]bool)
		for _, fd := range diff.Differences {
			fieldNames[fd.Field] = true
		}
		require.True(t, fieldNames["lockReason"], "expected 'lockReason' field in diff")
		require.True(t, fieldNames["scope"], "expected 'scope' field in diff")

		err = eng.AcceptRemoteMetadata("feature-a")
		require.NoError(t, err)

		require.True(t, eng.IsLocked(branch), "expected branch to be locked after accepting remote")
		require.Equal(t, "remote-scope", eng.GetScope(branch).String(), "expected scope to match remote after accepting")
	})

	t.Run("no conflict when local equals remote", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		sh.CreateBranch("feature-b").
			CommitChange("file-b", "content-b").
			TrackBranch("feature-b", "main")

		eng := sh.Engine

		branch := eng.GetBranch("feature-b")
		_, err := eng.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonUser)
		require.NoError(t, err)
		require.NoError(t, eng.SetScope(context.Background(), branch, engine.NewScope("same-scope")))

		remoteMeta := git.NewMetaFrom(git.MetaFields{
			LockReason: git.LockReasonUser,
			Scope:      new("same-scope"),
		})
		createRemoteMetadataRef(t, sh, "feature-b", remoteMeta)

		err = eng.LoadRemoteMetadataCache()
		require.NoError(t, err)

		diff, err := eng.ComputeMetadataDiff("feature-b")
		require.NoError(t, err)
		require.NotNil(t, diff)
		require.False(t, diff.HasConflict, "expected no conflict when local equals remote")
		require.Empty(t, diff.Differences)
	})

	t.Run("detects orphaned local metadata", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		sh.CreateBranch("feature-c").
			CommitChange("file-c", "content-c").
			TrackBranch("feature-c", "main")

		eng := sh.Engine

		branch := eng.GetBranch("feature-c")
		_, err := eng.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonUser)
		require.NoError(t, err)

		err = eng.SetLastModifiedBy("feature-c")
		require.NoError(t, err)

		err = eng.LoadRemoteMetadataCache()
		require.NoError(t, err)

		orphaned, err := eng.FindOrphanedLocalMetadata()
		require.NoError(t, err)
		require.Len(t, orphaned, 1, "expected 1 orphaned metadata entry")
		require.Equal(t, "feature-c", orphaned[0].BranchName)
	})

	t.Run("HasLocalModifications detects changes since sync", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		sh.CreateBranch("feature-d").
			CommitChange("file-d", "content-d").
			TrackBranch("feature-d", "main")

		eng := sh.Engine
		branch := eng.GetBranch("feature-d")

		require.False(t, eng.HasLocalModifications("feature-d"))

		err := eng.SetLastModifiedBy("feature-d")
		require.NoError(t, err)

		require.False(t, eng.HasLocalModifications("feature-d"))

		_, err = eng.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonUser)
		require.NoError(t, err)

		require.True(t, eng.HasLocalModifications("feature-d"))
	})

	t.Run("ignores remote metadata for non-existent local branches", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		eng := sh.Engine

		remoteMeta := git.NewMetaFrom(git.MetaFields{
			LockReason: git.LockReasonUser,
			Scope:      new("remote-scope"),
		})
		createRemoteMetadataRef(t, sh, "non-existent-branch", remoteMeta)

		err := eng.LoadRemoteMetadataCache()
		require.NoError(t, err)

		diffs, err := eng.ComputeAllMetadataDiffs()
		require.NoError(t, err)
		require.Empty(t, diffs, "expected no diffs for non-existent local branches")
	})

	t.Run("identifies orphaned metadata when local branch is gone", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		eng := sh.Engine

		sh.CreateBranch("temp-branch").
			CommitChange("temp-file", "content").
			TrackBranch("temp-branch", "main")

		sh.Checkout("main")
		err := sh.Scene.Repo.RunGitCommand("branch", "-D", "temp-branch")
		require.NoError(t, err)

		sh.Rebuild()

		orphaned, err := eng.FindOrphanedLocalMetadata()
		require.NoError(t, err)

		found := false
		for _, info := range orphaned {
			if info.BranchName == "temp-branch" {
				found = true
				require.False(t, info.ExistsLocally)
				require.Equal(t, engine.OrphanedActionDelete, info.Action)
				break
			}
		}
		require.True(t, found, "expected to find orphaned metadata for deleted branch")
	})
}

// createRemoteMetadataRef creates a ref at refs/stackit/remote-metadata/<branch> to simulate fetched remote metadata.
func createRemoteMetadataRef(t *testing.T, sh *scenario.Scenario, branchName string, meta *git.Meta) {
	t.Helper()

	data, err := json.Marshal(meta)
	require.NoError(t, err)

	tmpFile := filepath.Join(sh.Scene.Dir, ".git", "tmp-meta-"+branchName)
	err = os.WriteFile(tmpFile, data, 0600)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	blobSha, err := sh.Scene.Repo.RunGitCommandAndGetOutput("hash-object", "-w", tmpFile)
	require.NoError(t, err)

	blobSha = strings.TrimRight(blobSha, "\n")

	refName := "refs/stackit/remote-metadata/" + branchName
	err = sh.Scene.Repo.RunGitCommand("update-ref", refName, blobSha)
	require.NoError(t, err)
}
