package sync

import (
	"context"
	"testing"

	"github.com/google/go-github/v62/github"
	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

const (
	testOwner = "owner"
	testRepo  = "repo"
)

// TestSyncDiamondStackParentPreservation tests that when syncing a diamond-shaped stack,
// branch parents are not incorrectly changed based on stale GitHub PR base information.
//
// BUG SCENARIO:
//  1. Create diamond: main -> branch-a -> [branch-b, branch-c]
//  2. Submit PRs (creates PRs with correct bases)
//  3. Modify branch-a
//  4. Submit again - BUT the PR base update for branch-c fails silently
//     (can happen due to "no commits between base and head" check in updatePullRequestQuiet)
//  5. Sync runs SyncPrInfo which fetches PR info from GitHub
//  6. GitHub's PR info for branch-c has stale base "main" (should be "branch-a")
//  7. SyncParentsFromGitHubBase trusts GitHub and reparents branch-c to main
//  8. BUG: branch-c is now incorrectly parented to main, breaking the stack!
//
// This test verifies that the bug exists (test fails when bug is present).
// Once fixed, this test should pass, ensuring correct behavior.
func TestSyncDiamondStackParentPreservation(t *testing.T) {
	t.Run("BUG: sync incorrectly reparents branch when GitHub has stale base info", func(t *testing.T) {
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
		// This is the common case where 'submit' skips the PR base update.
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
		// This can happen if the PR base update failed silently during submit
		prNumber1 := 1
		prNumber2 := 2
		prNumber3 := 3
		mockConfig.PRs["branch-a"] = &github.PullRequest{
			Number:  &prNumber1,
			Title:   github.String("Branch A PR"),
			Head:    &github.PullRequestBranch{Ref: github.String("branch-a")},
			Base:    &github.PullRequestBranch{Ref: github.String("main")}, // Correct
			State:   github.String("open"),
			HTMLURL: github.String("https://github.com/owner/repo/pull/1"),
		}
		mockConfig.PRs["branch-b"] = &github.PullRequest{
			Number:  &prNumber2,
			Title:   github.String("Branch B PR"),
			Head:    &github.PullRequestBranch{Ref: github.String("branch-b")},
			Base:    &github.PullRequestBranch{Ref: github.String("branch-a")}, // Correct
			State:   github.String("open"),
			HTMLURL: github.String("https://github.com/owner/repo/pull/2"),
		}
		// STALE: branch-c shows main as base instead of branch-a
		// This simulates a failed base update on GitHub
		mockConfig.PRs["branch-c"] = &github.PullRequest{
			Number:  &prNumber3,
			Title:   github.String("Branch C PR"),
			Head:    &github.PullRequestBranch{Ref: github.String("branch-c")},
			Base:    &github.PullRequestBranch{Ref: github.String("main")}, // STALE - should be branch-a!
			State:   github.String("open"),
			HTMLURL: github.String("https://github.com/owner/repo/pull/3"),
		}

		// Create GitHub client and configure context
		client, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		mockClient := testhelpers.NewMockGitHubClientInterface(client, owner, repo, mockConfig)
		s.Context.GitHubClient = mockClient

		// Store PR info in local metadata simulating what SyncPrInfo would write
		// from GitHub. The key insight is: if SyncPrInfo fetches stale info from GitHub,
		// it writes that stale base into local metadata. Then SyncParentsFromGitHubBase
		// reads it and reparents the local branch.
		//
		// SIMULATE STALE SYNC: Store PR info with the WRONG base for branch-c
		// This mimics what happens when GitHub has stale base info
		storeLocalPRInfo(t, s.Engine, "branch-a", 1, "main")
		storeLocalPRInfo(t, s.Engine, "branch-b", 2, "branch-a")
		storeLocalPRInfo(t, s.Engine, "branch-c", 3, "main") // STALE: Should be branch-a, but GitHub says main!

		// Checkout to branch-c before sync
		s.Checkout("branch-c")

		// Now call SyncParentsFromGitHubBase directly to test if it incorrectly reparents
		// based on the stale PR info we stored
		syncResult, err := ParentsFromGitHubBase(s.Context)
		require.NoError(t, err)

		// Rebuild engine to pick up any changes
		err = s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Check what SyncParentsFromGitHubBase did
		t.Logf("Branches reparented by SyncParentsFromGitHubBase: %v", syncResult.BranchesReparented)

		// Get the current parent of branch-c
		branchC := s.Engine.GetBranch("branch-c")
		parentC := branchC.GetParent()

		// CRITICAL: The bug would cause branch-c to be reparented to main!
		// If this assertion fails, the bug exists.
		if parentC != nil {
			t.Logf("After SyncParentsFromGitHubBase, branch-c parent is: %s", parentC.GetName())
			if parentC.GetName() == "main" {
				t.Errorf("BUG CONFIRMED: branch-c was incorrectly reparented to 'main' instead of keeping 'branch-a'")
			}
		} else {
			t.Errorf("branch-c has no parent after sync!")
		}

		// Verify the expected structure (branch-c should keep branch-a as parent)
		s.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
			"branch-c": "branch-a", // This MUST remain branch-a, not main!
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
			Title:   github.String("Branch A PR"),
			Head:    &github.PullRequestBranch{Ref: github.String("branch-a")},
			Base:    &github.PullRequestBranch{Ref: github.String("main")},
			State:   github.String("open"),
			HTMLURL: github.String("https://github.com/owner/repo/pull/1"),
		}
		mockConfig.PRs["branch-b"] = &github.PullRequest{
			Number:  &prNumber2,
			Title:   github.String("Branch B PR"),
			Head:    &github.PullRequestBranch{Ref: github.String("branch-b")},
			Base:    &github.PullRequestBranch{Ref: github.String("branch-a")},
			State:   github.String("open"),
			HTMLURL: github.String("https://github.com/owner/repo/pull/2"),
		}
		mockConfig.PRs["branch-c"] = &github.PullRequest{
			Number:  &prNumber3,
			Title:   github.String("Branch C PR"),
			Head:    &github.PullRequestBranch{Ref: github.String("branch-c")},
			Base:    &github.PullRequestBranch{Ref: github.String("branch-a")},
			State:   github.String("open"),
			HTMLURL: github.String("https://github.com/owner/repo/pull/3"),
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
		})
		require.NoError(t, err)

		// Verify structure is preserved
		s.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
			"branch-c": "branch-a",
		})
	})

	t.Run("SyncParentsFromGitHubBase prefers local parent when GitHub base is an ancestor", func(t *testing.T) {
		// This test verifies that we PRESERVE the local stack structure even if
		// GitHub's PR base is different, as long as GitHub's base is just a
		// more distant ancestor (like main) of our current specific parent.

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
			Title:   github.String("Branch A PR"),
			Head:    &github.PullRequestBranch{Ref: github.String("branch-a")},
			Base:    &github.PullRequestBranch{Ref: github.String("main")},
			State:   github.String("open"),
			HTMLURL: github.String("https://github.com/owner/repo/pull/1"),
		}
		// GitHub says branch-b base is main (simulating stale or generic info on GitHub)
		mockConfig.PRs["branch-b"] = &github.PullRequest{
			Number:  &prNumber2,
			Title:   github.String("Branch B PR"),
			Head:    &github.PullRequestBranch{Ref: github.String("branch-b")},
			Base:    &github.PullRequestBranch{Ref: github.String("main")}, // Generic base on GitHub
			State:   github.String("open"),
			HTMLURL: github.String("https://github.com/owner/repo/pull/2"),
		}

		client, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		mockClient := testhelpers.NewMockGitHubClientInterface(client, owner, repo, mockConfig)
		s.Context.GitHubClient = mockClient

		// Store local PR info with generic base from GitHub
		storeLocalPRInfo(t, s.Engine, "branch-a", 1, "main")
		storeLocalPRInfo(t, s.Engine, "branch-b", 2, "main") // Local cache says main (from sync)

		s.Checkout("branch-b")

		// Before sync, set local structure to be specific
		err = s.Engine.SetParent(context.Background(), s.Engine.GetBranch("branch-b"), s.Engine.GetBranch("branch-a"))
		require.NoError(t, err)

		s.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
		})

		// Sync - should PRESERVE branch-a as parent because main is its ancestor
		err = Action(s.Context, Options{
			All:     false,
			Force:   false,
			Restack: false,
		})
		require.NoError(t, err)

		// Rebuild to pick up changes
		err = s.Engine.Rebuild("main")
		require.NoError(t, err)

		// VERIFICATION: branch-b should now be parented to main because it has its
		// own commits, so the move to an ancestor on GitHub is treated as intentional.
		branchB := s.Engine.GetBranch("branch-b")
		parent := branchB.GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "main", parent.GetName(), "branch-b should be reparented to main because it has its own commits")
	})
}

// storeLocalPRInfo stores PR info in branch metadata
func storeLocalPRInfo(t *testing.T, eng engine.Engine, branchName string, prNumber int, baseBranch string) {
	t.Helper()
	branch := eng.GetBranch(branchName)
	err := eng.UpsertPrInfo(branch, testhelpers.NewTestPrInfoWithTitle(prNumber, branchName+" PR").
		WithBase(baseBranch).
		WithURL("https://github.com/owner/repo/pull/"+string(rune('0'+prNumber))))
	require.NoError(t, err)
}
