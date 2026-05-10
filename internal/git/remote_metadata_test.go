package git_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestBatchDeleteRemoteMetadataRefs(t *testing.T) {
	t.Run("deletes multiple remote metadata refs", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// Create a bare remote
		_, err := scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Create some metadata refs and push them
		branches := []string{"branch1", "branch2", "branch3"}
		runner := git.NewRunnerWithPath(scene.Dir, nil)
		for _, b := range branches {
			refName := fmt.Sprintf("refs/stackit/metadata/%s", b)
			// Create a blob for the ref
			sha, err := runner.CreateBlob(fmt.Sprintf(`{"branch":"%s"}`, b))
			require.NoError(t, err)

			err = scene.Repo.RunGitCommand("update-ref", refName, sha)
			require.NoError(t, err)

			err = scene.Repo.RunGitCommand("push", "origin", refName)
			require.NoError(t, err)
		}

		// Verify refs exist on remote
		for _, b := range branches {
			_, err := scene.Repo.RunGitCommandAndGetOutput("ls-remote", "origin", fmt.Sprintf("refs/stackit/metadata/%s", b))
			require.NoError(t, err)
		}

		// Delete multiple refs
		err = runner.BatchDeleteRemoteMetadataRefs(context.Background(), branches[0:2]) // branch1, branch2
		require.NoError(t, err)

		// Verify branch1 and branch2 are gone from remote, but branch3 remains
		out1, _ := scene.Repo.RunGitCommandAndGetOutput("ls-remote", "origin", "refs/stackit/metadata/branch1")
		require.Empty(t, out1)

		out2, _ := scene.Repo.RunGitCommandAndGetOutput("ls-remote", "origin", "refs/stackit/metadata/branch2")
		require.Empty(t, out2)

		out3, err := scene.Repo.RunGitCommandAndGetOutput("ls-remote", "origin", "refs/stackit/metadata/branch3")
		require.NoError(t, err)
		require.NotEmpty(t, out3)
	})

	t.Run("handles single ref deletion", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		_, err := scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		refName := "refs/stackit/metadata/branch1"
		runner := git.NewRunnerWithPath(scene.Dir, nil)
		sha, err := runner.CreateBlob(`{"branch":"branch1"}`)
		require.NoError(t, err)

		err = scene.Repo.RunGitCommand("update-ref", refName, sha)
		require.NoError(t, err)

		err = scene.Repo.RunGitCommand("push", "origin", refName)
		require.NoError(t, err)

		err = runner.BatchDeleteRemoteMetadataRefs(context.Background(), []string{"branch1"})
		require.NoError(t, err)

		out, _ := scene.Repo.RunGitCommandAndGetOutput("ls-remote", "origin", refName)
		require.Empty(t, out)
	})

	t.Run("handles empty slice gracefully", func(t *testing.T) {
		runner := git.NewRunner(nil)
		err := runner.BatchDeleteRemoteMetadataRefs(context.Background(), []string{})
		require.NoError(t, err)
	})
}
