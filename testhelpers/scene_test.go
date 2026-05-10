package testhelpers_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
)

func TestInitialCommitSceneSetupTemplate(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	messages, err := scene.Repo.ListCurrentBranchCommitMessages()
	require.NoError(t, err)
	require.Equal(t, []string{"initial"}, messages)
}

func TestNewRemoteSceneParallel(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewRemoteSceneParallel(t)

	remoteURL, err := scene.Repo.RunGitCommandAndGetOutput("remote", "get-url", "origin")
	require.NoError(t, err)
	require.NotEmpty(t, remoteURL)

	upstream, err := scene.Repo.RunGitCommandAndGetOutput("rev-parse", "--abbrev-ref", "main@{upstream}")
	require.NoError(t, err)
	require.Equal(t, "origin/main", upstream)
}
