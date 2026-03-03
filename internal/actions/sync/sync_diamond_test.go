package sync

import (
	"context"
	"testing"

	"github.com/google/go-github/v62/github"
	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	stackitgithub "stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

const (
	testOwner = "owner"
	testRepo  = "repo"
)

// TestSyncDiamondStackParentPreservation tests that when syncing a diamond-shaped stack,
// local parent relationships are preserved and pushed to GitHub (local is authoritative).
//
// PREVIOUS BUG SCENARIO (now fixed):
//  1. Create diamond: main -> branch-a -> [branch-b, branch-c]
//  2. Submit PRs (creates PRs with correct bases)
//  3. Modify branch-a
//  4. Submit again - BUT the PR base update for branch-c fails silently
//  5. GitHub's PR info for branch-c has stale base "main" (should be "branch-a")
//  6. Old behavior: SyncParentsFromGitHubBase trusted GitHub and reparented branch-c to main
//  7. New behavior: PushParentsToGitHub pushes local parent to GitHub (local is authoritative)
func TestSyncDiamondStackParentPreservation(t *testing.T) {
	// Set dummy GITHUB_TOKEN to avoid calling 'gh auth token' and triggering credentials prompts
	t.Setenv("GITHUB_TOKEN", "dummy")

	t.Run("sync preserves local parent and pushes to GitHub when GitHub has stale base", func(t *testing.T) {
		// Setup scenario with diamond structure
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Set up a local bare remote to avoid network calls and credentials prompts.
		// SyncAction calls PullTrunk which requires a remote.
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		// Create diamond:
		// main -> branch-a -> [branch-b, branch-c]
		// branch-a has its own commit.
		// branch-b has its own commit.
		// branch-c is EMPTY relative to branch-a (at the same commit).
		s.CreateBranch("branch-a").
			CommitChange("file-a", "branch-a initial commit").
			TrackBranch("branch-a", "main")

		s.CreateBranch("branch-b").
			CommitChange("file-b", "branch-b commit").
			TrackBranch("branch-b", "branch-a")

		s.Checkout("branch-a").
			CreateBranch("branch-c").
			TrackBranch("branch-c", "branch-a")

		// Verify initial structure
		s.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
			"branch-c": "branch-a",
		})

		// Setup mock GitHub server with PRs that have STALE base info
		// This simulates the scenario where GitHub PR base update failed
		mockConfig := testhelpers.NewMockGitHubServerConfig()
		mockConfig.Owner = testOwner
		mockConfig.Repo = testRepo

		// Simulate stale PRs where branch-c has wrong base (main instead of branch-a)
		prNumber1 := 1
		prNumber2 := 2
		prNumber3 := 3
		mockConfig.PRs["branch-a"] = &github.PullRequest{
			Number:  &prNumber1,
			Title:   new("Branch A PR"),
			Head:    &github.PullRequestBranch{Ref: new("branch-a")},
			Base:    &github.PullRequestBranch{Ref: new("main")}, // Correct
			State:   new("open"),
			HTMLURL: new("https://github.com/owner/repo/pull/1"),
		}
		mockConfig.PRs["branch-b"] = &github.PullRequest{
			Number:  &prNumber2,
			Title:   new("Branch B PR"),
			Head:    &github.PullRequestBranch{Ref: new("branch-b")},
			Base:    &github.PullRequestBranch{Ref: new("branch-a")}, // Correct
			State:   new("open"),
			HTMLURL: new("https://github.com/owner/repo/pull/2"),
		}
		// STALE: branch-c shows main as base instead of branch-a
		mockConfig.PRs["branch-c"] = &github.PullRequest{
			Number:  &prNumber3,
			Title:   new("Branch C PR"),
			Head:    &github.PullRequestBranch{Ref: new("branch-c")},
			Base:    &github.PullRequestBranch{Ref: new("main")}, // STALE - should be branch-a!
			State:   new("open"),
			HTMLURL: new("https://github.com/owner/repo/pull/3"),
		}

		// Create GitHub client and configure context
		client, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		mockClient := testhelpers.NewMockGitHubClientInterface(client, owner, repo, mockConfig)
		s.Context.GitHubClient = mockClient

		// Checkout to branch-c before sync
		s.Checkout("branch-c")

		// Call PushParentsToGitHub - this should push local parent (branch-a) to GitHub
		// and NOT modify local parent based on GitHub's stale base
		githubResult := &GitHubSyncResult{
			BranchNames: []string{"branch-a", "branch-b", "branch-c"},
			RepoOwner:   testOwner,
			RepoName:    testRepo,
			PRInfos: map[string]*stackitgithub.PullRequestInfo{
				"branch-a": {Number: prNumber1, Base: "main", State: "open"},
				"branch-b": {Number: prNumber2, Base: "branch-a", State: "open"},
				"branch-c": {Number: prNumber3, Base: "main", State: "open"}, // STALE
			},
		}

		syncResult, err := PushParentsToGitHub(s.Context, githubResult, nil)
		require.NoError(t, err)

		// Verify that branch-c's PR base was updated on GitHub
		t.Logf("Branches with PR base updated on GitHub: %v", syncResult.BranchesUpdated)
		require.Contains(t, syncResult.BranchesUpdated, "branch-c", "branch-c should have its PR base updated on GitHub")

		// Rebuild engine
		err = s.Engine.Rebuild("main")
		require.NoError(t, err)

		// CRITICAL: Local parent should be PRESERVED (not changed by sync)
		branchC := s.Engine.GetBranch("branch-c")
		parentC := branchC.GetParent()
		require.NotNil(t, parentC)
		require.Equal(t, "branch-a", parentC.GetName(), "branch-c local parent should remain branch-a")

		// Verify the expected structure is preserved
		s.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
			"branch-c": "branch-a", // Local parent preserved!
		})
	})

	t.Run("sync after modify preserves correct parents", func(t *testing.T) {
		// This test simulates the full user scenario:
		// 1. Create diamond
		// 2. Submit
		// 3. Modify branch-a
		// 4. Sync
		// 5. Verify parents preserved

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Set up a local bare remote to avoid network calls and credentials prompts.
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		// Create diamond structure with actual file changes
		s.CreateBranch("branch-a").
			CommitChange("file-a", "branch-a initial").
			TrackBranch("branch-a", "main")

		s.CreateBranch("branch-b").
			CommitChange("file-b", "branch-b commit").
			TrackBranch("branch-b", "branch-a")

		s.Checkout("branch-a").
			CreateBranch("branch-c").
			CommitChange("file-c", "branch-c commit").
			TrackBranch("branch-c", "branch-a")

		// Verify initial structure
		s.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
			"branch-c": "branch-a",
		})

		// Simulate modify on branch-a (add another file change)
		s.Checkout("branch-a").
			CommitChange("file-a-modified", "branch-a modified")

		// Rebuild to update metadata
		err = s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Manually restack children (simulating what modify would do)
		branchB := s.Engine.GetBranch("branch-b")
		branchC := s.Engine.GetBranch("branch-c")
		_, err = s.Engine.RestackBranches(context.Background(), []engine.Branch{branchB})
		require.NoError(t, err)
		_, err = s.Engine.RestackBranches(context.Background(), []engine.Branch{branchC})
		require.NoError(t, err)

		// Now setup mock GitHub with potentially stale info
		mockConfig := testhelpers.NewMockGitHubServerConfig()
		mockConfig.Owner = testOwner
		mockConfig.Repo = testRepo
		prNumber1 := 1
		prNumber2 := 2
		prNumber3 := 3

		// All PRs have correct bases in this test
		mockConfig.PRs["branch-a"] = &github.PullRequest{
			Number:  &prNumber1,
			Title:   new("Branch A PR"),
			Head:    &github.PullRequestBranch{Ref: new("branch-a")},
			Base:    &github.PullRequestBranch{Ref: new("main")},
			State:   new("open"),
			HTMLURL: new("https://github.com/owner/repo/pull/1"),
		}
		mockConfig.PRs["branch-b"] = &github.PullRequest{
			Number:  &prNumber2,
			Title:   new("Branch B PR"),
			Head:    &github.PullRequestBranch{Ref: new("branch-b")},
			Base:    &github.PullRequestBranch{Ref: new("branch-a")},
			State:   new("open"),
			HTMLURL: new("https://github.com/owner/repo/pull/2"),
		}
		mockConfig.PRs["branch-c"] = &github.PullRequest{
			Number:  &prNumber3,
			Title:   new("Branch C PR"),
			Head:    &github.PullRequestBranch{Ref: new("branch-c")},
			Base:    &github.PullRequestBranch{Ref: new("branch-a")},
			State:   new("open"),
			HTMLURL: new("https://github.com/owner/repo/pull/3"),
		}

		client, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		mockClient := testhelpers.NewMockGitHubClientInterface(client, owner, repo, mockConfig)
		s.Context.GitHubClient = mockClient

		// Store local PR info
		storeLocalPRInfo(t, s.Engine, "branch-a", 1, "main")
		storeLocalPRInfo(t, s.Engine, "branch-b", 2, "branch-a")
		storeLocalPRInfo(t, s.Engine, "branch-c", 3, "branch-a")

		s.Checkout("branch-a")

		// Sync
		err = Action(s.Context, Options{
			All:     false,
			Force:   false,
			Restack: true, // Include restack this time
		}, nil)
		require.NoError(t, err)

		// Verify structure is preserved
		s.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
			"branch-c": "branch-a",
		})
	})

	t.Run("sync pushes local parent to GitHub when GitHub base differs", func(t *testing.T) {
		// This test verifies that local parent is authoritative:
		// - Local parent is branch-a
		// - GitHub PR base is main (stale/different)
		// - Sync should push local parent to GitHub, not change local

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Set up a local bare remote to avoid network calls and credentials prompts.
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		// Create: main -> branch-a -> branch-b with actual file changes
		s.CreateBranch("branch-a").
			CommitChange("file-a", "branch-a commit").
			TrackBranch("branch-a", "main")

		s.CreateBranch("branch-b").
			CommitChange("file-b", "branch-b commit").
			TrackBranch("branch-b", "branch-a")

		// Setup mock GitHub with branch-b having MAIN as base (not branch-a)
		mockConfig := testhelpers.NewMockGitHubServerConfig()
		mockConfig.Owner = testOwner
		mockConfig.Repo = testRepo
		prNumber1 := 1
		prNumber2 := 2

		mockConfig.PRs["branch-a"] = &github.PullRequest{
			Number:  &prNumber1,
			Title:   new("Branch A PR"),
			Head:    &github.PullRequestBranch{Ref: new("branch-a")},
			Base:    &github.PullRequestBranch{Ref: new("main")},
			State:   new("open"),
			HTMLURL: new("https://github.com/owner/repo/pull/1"),
		}
		// GitHub says branch-b base is main (stale)
		mockConfig.PRs["branch-b"] = &github.PullRequest{
			Number:  &prNumber2,
			Title:   new("Branch B PR"),
			Head:    &github.PullRequestBranch{Ref: new("branch-b")},
			Base:    &github.PullRequestBranch{Ref: new("main")}, // Stale base on GitHub
			State:   new("open"),
			HTMLURL: new("https://github.com/owner/repo/pull/2"),
		}

		client, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		mockClient := testhelpers.NewMockGitHubClientInterface(client, owner, repo, mockConfig)
		s.Context.GitHubClient = mockClient

		s.Checkout("branch-b")

		// Verify initial structure (local parent is branch-a)
		s.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
		})

		// Sync - should PRESERVE branch-a as local parent and push to GitHub
		err = Action(s.Context, Options{
			All:     false,
			Force:   false,
			Restack: false,
		}, nil)
		require.NoError(t, err)

		// Rebuild to pick up changes
		err = s.Engine.Rebuild("main")
		require.NoError(t, err)

		// VERIFICATION: Local parent should be PRESERVED (branch-a, not changed to main)
		branchB := s.Engine.GetBranch("branch-b")
		parent := branchB.GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "branch-a", parent.GetName(), "branch-b local parent should remain branch-a (local is authoritative)")
	})
}

// storeLocalPRInfo stores PR info in branch metadata
func storeLocalPRInfo(t *testing.T, eng engine.Engine, branchName string, prNumber int, baseBranch string) {
	t.Helper()
	branch := eng.GetBranch(branchName)
	err := eng.UpsertPrInfo(context.Background(), branch, testhelpers.NewTestPrInfoWithTitle(prNumber, branchName+" PR").
		WithBase(baseBranch).
		WithURL("https://github.com/owner/repo/pull/"+string(rune('0'+prNumber))))
	require.NoError(t, err)
}
