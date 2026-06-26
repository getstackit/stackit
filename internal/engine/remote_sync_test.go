package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

const metadataFetchRefspec = "+refs/stackit/metadata/*:refs/stackit/remote-metadata/*"

// TestConfigureRemoteMetadataSyncResolvesNonOriginRemote pins the fix for the
// origin-hardcoded guard: ConfigureRemoteMetadataSync must configure the metadata
// refspec on the branch's actual tracking remote (here "upstream"), not skip it
// just because there is no "origin" remote. Skipping it reintroduced the #1330
// gap (a plain `git fetch` stops pulling branch metadata) for fork-style setups.
func TestConfigureRemoteMetadataSyncResolvesNonOriginRemote(t *testing.T) {
	t.Parallel()
	sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// A repo whose only remote is "upstream" and whose current branch tracks it.
	require.NoError(t, sh.Scene.Repo.RunGitCommand("remote", "add", "upstream", "https://example.com/repo.git"))
	require.NoError(t, sh.Scene.Repo.RunGitCommand("config", "branch.main.remote", "upstream"))

	require.NoError(t, sh.Engine.ConfigureRemoteMetadataSync(context.Background()))

	upstreamRefspecs, _ := sh.Engine.Git().GetConfigAll("remote.upstream.fetch")
	require.Contains(t, upstreamRefspecs, metadataFetchRefspec,
		"metadata refspec must be configured on the resolved (non-origin) remote")

	// origin never existed, so it must not have been polluted with a refspec.
	originRefspecs, _ := sh.Engine.Git().GetConfigAll("remote.origin.fetch")
	require.NotContains(t, originRefspecs, metadataFetchRefspec)
}

// TestConfigureRemoteMetadataSyncSkipsRemoteless asserts a remote-less repo is
// never polluted with a dangling remote.origin.fetch entry.
func TestConfigureRemoteMetadataSyncSkipsRemoteless(t *testing.T) {
	t.Parallel()
	sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	require.NoError(t, sh.Engine.ConfigureRemoteMetadataSync(context.Background()))

	refspecs, _ := sh.Engine.Git().GetConfigAll("remote.origin.fetch")
	require.NotContains(t, refspecs, metadataFetchRefspec)
}
