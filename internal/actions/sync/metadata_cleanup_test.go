package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestMetadataCleanup(t *testing.T) {
	t.Run("silently cleans up metadata for deleted local branches", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// 1. Create a branch and metadata
		s.CreateBranch("temp-branch").
			CommitChange("temp-file", "content").
			TrackBranch("temp-branch", "main")

		// Verify metadata exists
		refs, err := s.Engine.Git().ListMetadata()
		require.NoError(t, err)
		require.Contains(t, refs, "temp-branch")

		// 2. Simulate it was synced (sets LocalOnlyHash)
		err = s.Engine.SetLastModifiedBy("temp-branch")
		require.NoError(t, err)

		// 3. Delete the git branch but keep the metadata ref
		s.Checkout("main")
		err = s.Scene.Repo.RunGitCommand("branch", "-D", "temp-branch")
		require.NoError(t, err)

		// 4. Rebuild engine so it knows the branch is gone
		s.Rebuild()

		// 5. Run sync remote metadata
		err = syncRemoteMetadata(s.Context, &Options{}, &NullHandler{})
		require.NoError(t, err)

		// 6. Verify metadata ref for deleted branch is gone
		refs, err = s.Engine.Git().ListMetadata()
		require.NoError(t, err)
		require.NotContains(t, refs, "temp-branch", "metadata ref should have been cleaned up")
	})

	t.Run("does not prompt for remote metadata on non-existent branches", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// 1. Simulate remote metadata for a branch that doesn't exist locally
		remoteMeta := git.NewMetaFrom(git.MetaFields{
			LockReason: "locked",
		})

		// Serialize
		data, err := json.Marshal(remoteMeta)
		require.NoError(t, err)

		// Write to blob
		tmpFile := filepath.Join(s.Scene.Dir, ".git", "tmp-meta-remote")
		err = os.WriteFile(tmpFile, data, 0600)
		require.NoError(t, err)
		defer os.Remove(tmpFile)

		blobSha, err := s.Scene.Repo.RunGitCommandAndGetOutput("hash-object", "-w", tmpFile)
		require.NoError(t, err)

		// Trim
		if len(blobSha) > 40 {
			blobSha = blobSha[:40]
		} else if len(blobSha) > 0 && blobSha[len(blobSha)-1] == '\n' {
			blobSha = blobSha[:len(blobSha)-1]
		}

		// Create remote metadata ref
		err = s.Scene.Repo.RunGitCommand("update-ref", "refs/stackit/remote-metadata/remote-only-branch", blobSha)
		require.NoError(t, err)

		// 2. Run syncRemoteMetadata
		// This should NOT prompt (NullHandler returns defaults without prompting)
		err = syncRemoteMetadata(s.Context, &Options{}, &NullHandler{})
		require.NoError(t, err)
	})
}
