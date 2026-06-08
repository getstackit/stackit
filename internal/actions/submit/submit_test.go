package submit_test

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// countingRunner wraps a git.Runner and counts FetchRemoteShas calls. It lets
// tests assert that submit reads the remote ref list once for the whole stack
// rather than once per branch (a full `git ls-remote` each time).
type countingRunner struct {
	git.Runner
	fetchRemoteShas atomic.Int64
}

func (c *countingRunner) FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	c.fetchRemoteShas.Add(1)
	return c.Runner.FetchRemoteShas(ctx, remote)
}

// noopHandler is a test handler that ignores all events
type noopHandler struct{}

func (h *noopHandler) OnEvent(_ submit.Event)                          {}
func (h *noopHandler) Confirm(_ string, defaultYes bool) (bool, error) { return defaultYes, nil }
func (h *noopHandler) IsInteractive() bool                             { return false }

type captureStackHandler struct {
	stackEvent *submit.StackDisplayEvent
}

func (h *captureStackHandler) OnEvent(e submit.Event) {
	if ev, ok := e.(submit.StackDisplayEvent); ok {
		copied := ev
		h.stackEvent = &copied
	}
}

func (h *captureStackHandler) Confirm(_ string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

func (h *captureStackHandler) IsInteractive() bool { return false }

func TestActionWithMockedGitHub(t *testing.T) {
	t.Parallel()
	t.Run("creates PR for branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"feature": "main",
			})

		// Create a local remote to push to
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Create mocked GitHub client
		config := testhelpers.NewMockGitHubServerConfig()
		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
		githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

		// Create context with mocked client
		s.Context.GitHubClient = githubClient
		opts := submit.Options{
			DryRun: false, // We want to test actual PR creation
			NoEdit: true,  // Skip interactive prompts
			Draft:  true,  // Set draft status explicitly to skip prompt
		}

		// With mocked client, push is skipped, so this should succeed
		err = submit.Action(s.Context, opts, &noopHandler{})
		require.NoError(t, err, "Submit should succeed with mocked GitHub client")

		// Verify that PR was created in the mock
		require.Greater(t, len(config.CreatedPRs), 0, "Should have created at least one PR")
		require.Equal(t, "feature", *config.CreatedPRs[0].Head.Ref, "PR should be for feature branch")

		// Verify that metadata was updated with LastModifiedBy after submit
		meta, err := s.Engine.Git().ReadMetadata("feature")
		require.NoError(t, err, "Should be able to read metadata ref after submit")
		require.NotNil(t, meta.GetLastModifiedBy(), "LastModifiedBy should be set after submit")
		require.NotEmpty(t, meta.GetLastModifiedBy().GitName, "LastModifiedBy.GitName should not be empty")
		require.NotEmpty(t, meta.GetLastModifiedBy().GitEmail, "LastModifiedBy.GitEmail should not be empty")
	})

	t.Run("updates existing PR", func(t *testing.T) {
		t.Parallel()
		// Skip this test for now - branch tracking issue needs to be resolved separately
		t.Skip("Skipping due to branch tracking issue in test setup")

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"feature": "main",
			})

		// Create a local remote to push to
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Create mocked GitHub client with existing PR
		config := testhelpers.NewMockGitHubServerConfig()
		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
		githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

		// Pre-create a PR in the mock
		branchName := "feature"
		prNumber := 123
		prData := testhelpers.DefaultPRData()
		prData.Head = branchName
		prData.Number = prNumber
		pr := testhelpers.NewSamplePullRequest(prData)
		config.PRs[branchName] = pr
		config.CreatedPRs = append(config.CreatedPRs, pr)
		// Also add to UpdatedPRs so Get works
		config.UpdatedPRs[prNumber] = pr

		// Store PR info in engine
		branch := s.Engine.GetBranch(branchName)
		err = s.Engine.UpsertPrInfo(context.Background(), branch, testhelpers.NewTestPrInfoWithTitle(prNumber, prData.Title).
			WithBody(prData.Body).
			WithIsDraft(prData.Draft))
		require.NoError(t, err)

		// Create context with mocked client
		s.Context.GitHubClient = githubClient
		opts := submit.Options{
			DryRun: false,
			NoEdit: true,
		}

		// With mocked client, push is skipped, so this should succeed
		err = submit.Action(s.Context, opts, &noopHandler{})
		require.NoError(t, err, "Submit should succeed with mocked GitHub client")

		// Verify that PR was updated in the mock
		require.Greater(t, len(config.UpdatedPRs), 0, "Should have updated at least one PR")
		updatedPR, exists := config.UpdatedPRs[prNumber]
		require.True(t, exists, "PR %d should be in UpdatedPRs", prNumber)
		require.NotNil(t, updatedPR, "Updated PR should not be nil")
	})

	t.Run("submits entire branching stack with --stack flag", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"P":  "main",
				"C1": "P",
				"C2": "P",
			})

		// Move back to P
		s.Checkout("P")

		// Create a local remote
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Create mocked GitHub client
		mockConfig := testhelpers.NewMockGitHubServerConfig()
		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

		s.Context.GitHubClient = githubClient

		// Submit with --stack flag from branch P
		opts := submit.Options{
			StackRange: engine.StackRangeFull(),
			NoEdit:     true,
			Draft:      true,
		}

		err = submit.Action(s.Context, opts, &noopHandler{})
		require.NoError(t, err)

		// Should have created 3 PRs: P, C1, and C2
		require.Equal(t, 3, len(mockConfig.CreatedPRs), "Should have created PRs for P and its children C1, C2")

		// Verify branches are correct
		createdBranches := make(map[string]bool)
		for _, pr := range mockConfig.CreatedPRs {
			createdBranches[*pr.Head.Ref] = true
		}
		require.True(t, createdBranches["P"])
		require.True(t, createdBranches["C1"])
		require.True(t, createdBranches["C2"])

		// Verify that metadata was updated for all submitted branches
		for _, branchName := range []string{"P", "C1", "C2"} {
			meta, err := s.Engine.Git().ReadMetadata(branchName)
			require.NoError(t, err, "Should be able to read metadata for %s", branchName)
			require.NotNil(t, meta.GetLastModifiedBy(), "LastModifiedBy should be set for %s", branchName)
		}
	})

	t.Run("skips base update when no commits between base and head", func(t *testing.T) {
		t.Parallel()
		// This test covers the scenario where after reordering, a branch has no commits
		// between it and its new base, which would cause GitHub to reject the PR update.
		// Our fix should detect this and skip the base update to avoid the error.
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"A": "main",
				"B": "A",
			})

		// Create a local remote
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Get the SHA of branch A
		aSHA, err := s.Engine.GetBranch("A").GetRevision()
		require.NoError(t, err)

		// Make branch B point to the same commit as A
		// This simulates the scenario where there are no commits between B and A
		s.Checkout("A")
		s.RunGit("branch", "-f", "B", aSHA)
		s.Rebuild()

		// Create mocked GitHub client
		config := testhelpers.NewMockGitHubServerConfig()
		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
		githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

		// Create a PR for B with A as the base (but B and A point to same commit)
		prNumberB := 101
		prDataB := testhelpers.DefaultPRData()
		prDataB.Head = "B"
		prDataB.Number = prNumberB
		prDataB.Base = "main" // Original base
		prB := testhelpers.NewSamplePullRequest(prDataB)
		config.PRs["B"] = prB
		config.CreatedPRs = append(config.CreatedPRs, prB)
		config.UpdatedPRs[prNumberB] = prB

		// Store PR info in engine with A as the base (simulating after reorder)
		branchB := s.Engine.GetBranch("B")
		err = s.Engine.UpsertPrInfo(context.Background(), branchB, testhelpers.NewTestPrInfoWithTitle(prNumberB, prDataB.Title).
			WithBody(prDataB.Body).
			WithBase("main")) // Will be changed to "A" in prepareBranchesForSubmit
		require.NoError(t, err)

		// Update parent relationship: B's parent is now A
		err = s.Engine.SetParent(context.Background(), s.Engine.GetBranch("B"), s.Engine.GetBranch("A"), engine.DivergenceRecompute)
		require.NoError(t, err)

		// Verify that B and A have the same SHA (no commits between them)
		bSHA, err := s.Engine.GetBranch("B").GetRevision()
		require.NoError(t, err)
		require.Equal(t, aSHA, bSHA, "B and A should point to the same commit")

		// Now try to submit B with A as the new base
		// Since B's SHA equals A's SHA, the base update should be skipped
		s.Context.GitHubClient = githubClient
		opts := submit.Options{
			DryRun: false,
			NoEdit: true,
			Draft:  true,
		}

		s.Checkout("B")
		err = submit.Action(s.Context, opts, &noopHandler{})
		require.NoError(t, err, "Submit should succeed even when base update is skipped due to no commits")

		// Verify that the PR was updated (other fields should be updated)
		updatedPR, exists := config.UpdatedPRs[prNumberB]
		require.True(t, exists, "PR %d should be in UpdatedPRs", prNumberB)
		require.NotNil(t, updatedPR, "Updated PR should not be nil")
	})

	t.Run("skips push when branch is already in sync with remote", func(t *testing.T) {
		// Regression test: when local SHA == remote SHA, a plain --force-with-lease push
		// causes the server to reject with "cannot lock ref: reference already exists".
		// pushBranchIfNeeded must detect the Matches() case and skip the push entirely.
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"feature": "main",
			})

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Push the branch to remote first (simulating a prior successful push)
		err = s.Scene.Repo.PushBranch("origin", "feature")
		require.NoError(t, err)

		config := testhelpers.NewMockGitHubServerConfig()
		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
		githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

		s.Context.GitHubClient = githubClient
		opts := submit.Options{
			DryRun: false,
			NoEdit: true,
			Draft:  true,
		}

		s.Checkout("feature")
		// Second submit with the branch already pushed — must not fail with a push rejection.
		err = submit.Action(s.Context, opts, &noopHandler{})
		require.NoError(t, err, "submit should succeed when branch is already in sync with remote")
	})
}

func TestSubmitReadsRemoteStatusOnceForStack(t *testing.T) {
	t.Parallel()
	// Regression test for the per-branch ls-remote N+1: submitting a stack must
	// read remote ref status once for the whole stack, not once per branch.
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"P":  "main",
			"C1": "P",
			"C2": "C1",
		})

	s.Checkout("P")

	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	// Build a second engine over the same repo with a runner that counts
	// FetchRemoteShas, then drive submit through it.
	counting := &countingRunner{Runner: git.NewRunnerWithPath(s.Scene.Dir, nil)}
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
		Git:      counting,
	})
	require.NoError(t, err)

	config := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
	githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

	ctx := app.NewContext(eng,
		app.WithRepoRoot(s.Scene.Dir),
		app.WithWriter(&bytes.Buffer{}),
		app.WithGlobalOptions(app.GlobalOptions{Verify: true}),
	)
	ctx.GitHubClient = githubClient

	opts := submit.Options{
		StackRange: engine.StackRangeFull(),
		NoEdit:     true,
		Draft:      true,
	}

	err = submit.Action(ctx, opts, &noopHandler{})
	require.NoError(t, err)
	require.Equal(t, 3, len(config.CreatedPRs), "should create a PR for each of P, C1, C2")

	require.Equal(t, int64(1), counting.fetchRemoteShas.Load(),
		"submit must read remote status once for the whole stack, not once per branch")
}

func TestSubmitPreservesLockStatus(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"feature": "main",
		})

	// Create a local remote to push to
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	// Create mocked GitHub client
	config := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
	githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

	// Lock the branch
	branch := s.Engine.GetBranch("feature")
	_, err = s.Engine.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonUser)
	require.NoError(t, err)
	require.True(t, branch.IsLocked())

	// Create context with mocked client
	s.Context.GitHubClient = githubClient
	opts := submit.Options{
		DryRun: false,
		NoEdit: true,
		Draft:  true,
	}

	err = submit.Action(s.Context, opts, &noopHandler{})
	require.NoError(t, err)

	// Verify that the branch is STILL locked
	branch = s.Engine.GetBranch("feature")
	require.True(t, branch.IsLocked(), "Branch should still be locked after submission")

	meta, err := s.Engine.Git().ReadMetadata("feature")
	require.NoError(t, err)
	require.Equal(t, git.LockReasonUser, meta.GetLockReason(), "Metadata LockReason field should be set")
}

func TestSubmitDryRunWithWorktreeAnchorParent(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"wt-anchor": "main",
			"feature":   "wt-anchor",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("wt-anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	s.Checkout("feature")

	opts := submit.Options{
		DryRun: true,
		NoEdit: true,
	}

	err = submit.Action(s.Context, opts, &noopHandler{})
	require.NoError(t, err, "submit dry-run should treat worktree-anchor parent as trunk for validation/base resolution")
}

func TestSubmitDisplayTreeSkipsWorktreeAnchorParent(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"wt-anchor": "main",
			"feature":   "wt-anchor",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("wt-anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	s.Checkout("feature")

	handler := &captureStackHandler{}
	opts := submit.Options{
		DryRun: true,
		NoEdit: true,
	}

	err = submit.Action(s.Context, opts, handler)
	require.NoError(t, err)
	require.NotNil(t, handler.stackEvent)

	stack := handler.stackEvent.Stack
	require.Equal(t, []string{"feature"}, stack.Branches, "worktree anchors are not submittable")
	require.Equal(t, "main", stack.ParentMap["feature"], "display parent should skip worktree anchor")
	require.Equal(t, []string{"feature"}, stack.ChildrenMap["main"], "feature should appear as trunk child in display tree")
	require.Empty(t, stack.ChildrenMap["wt-anchor"], "worktree anchor should not appear in display tree relationships")

	_, hasAnchorFixedState := handler.stackEvent.FixedMap["wt-anchor"]
	require.False(t, hasAnchorFixedState, "fixed map should not include non-submittable worktree anchors")
}
