package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// noopHandler is a test handler that ignores all events
type noopHandler struct{}

func (h *noopHandler) OnEvent(_ submit.Event)                          {}
func (h *noopHandler) Confirm(_ string, defaultYes bool) (bool, error) { return defaultYes, nil }
func (h *noopHandler) IsInteractive() bool                             { return false }

func TestScopeRequiredInPattern(t *testing.T) {
	t.Parallel()

	t.Run("errors in non-interactive mode when pattern has scope but no scope provided", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Set a pattern that requires scope
		sh.Run("config set branch.pattern \"{scope}/{message}\"")

		// Create a file to have something to commit
		sh.Write("test.txt", "content")

		// Try to create a branch without providing scope - should error
		sh.RunExpectError("create -m 'test feature'").
			OutputContains("branch pattern contains {scope} but no scope provided")
	})

	t.Run("succeeds when pattern has scope and scope is provided via flag", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Set a pattern that requires scope
		sh.Run("config set branch.pattern \"{scope}/{message}\"")

		// Create a file to have something to commit
		sh.Write("test.txt", "content")

		// Create a branch with scope flag - should succeed
		sh.Run("create -m 'test feature' --scope JIRA-123").
			OutputContains("Created branch")

		// Verify the branch name contains the scope
		sh.OnBranch("JIRA-123/test-feature")
	})

	t.Run("succeeds when scope is inherited from parent", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Set a pattern that requires scope
		sh.Run("config set branch.pattern \"{scope}/{message}\"")

		// Create parent branch with scope
		sh.Write("parent.txt", "parent content")
		sh.Run("create -m 'parent' --scope PROJ-100")
		sh.OnBranch("PROJ-100/parent")

		// Create child branch - should inherit scope from parent
		sh.Write("child.txt", "child content")
		sh.Run("create -m 'child feature'")
		sh.OnBranch("PROJ-100/child-feature")
	})
}

func TestScopeSubmitSyncFlow(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)

	// 1. Create a branch
	sh.CreateBranch("feature").
		CommitChange("file", "content").
		TrackBranch("feature", "main")

	eng := sh.Engine
	branch := eng.GetBranch("feature")

	// 2. Set a scope locally
	require.NoError(t, eng.SetScope(context.Background(), branch, engine.NewScope("JIRA-123")))
	require.Equal(t, "JIRA-123", eng.GetScope(branch).String())

	// 3. Setup mocked GitHub client for submit
	config := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
	githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)
	sh.Context.GitHubClient = githubClient

	// 4. Submit the branch (this should push the metadata ref)
	opts := submit.Options{
		NoEdit: true,
		Draft:  true,
	}
	err := submit.Action(sh.Context, opts, &noopHandler{})
	require.NoError(t, err)

	// 5. Run sync to fetch remote metadata
	// This will fetch refs/stackit/metadata/*:refs/stackit/remote-metadata/*
	err = sync.Action(sh.Context, sync.Options{}, nil)
	require.NoError(t, err)

	// 6. Verify the scope is now in the remote metadata cache
	err = eng.LoadRemoteMetadataCache()
	require.NoError(t, err)

	cache := eng.GetRemoteMetadataCache()
	require.NotNil(t, cache.Get("feature"), "Remote metadata for 'feature' should exist in cache")
	require.NotNil(t, cache.Get("feature").GetScope(), "Scope should be set in remote metadata")
	require.Equal(t, "JIRA-123", *cache.Get("feature").GetScope(), "Remote metadata scope should match JIRA-123")
}
