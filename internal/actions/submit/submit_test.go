package submit_test

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/app"
	stackitconfig "github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// countingRunner wraps a git.Runner and counts the network-bound operations
// submit fans out over a stack. It lets tests assert that submit reads the
// remote ref list once (not once per branch) and pushes the whole stack in a
// single batched invocation rather than one `git push` per branch.
type countingRunner struct {
	git.Runner
	fetchRemoteShas atomic.Int64
	pushBranches    atomic.Int64
	pushBranch      atomic.Int64
}

func (c *countingRunner) FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	c.fetchRemoteShas.Add(1)
	return c.Runner.FetchRemoteShas(ctx, remote)
}

func (c *countingRunner) PushBranches(ctx context.Context, remote string, specs []git.PushSpec, opts git.PushOptions) map[string]error {
	c.pushBranches.Add(1)
	return c.Runner.PushBranches(ctx, remote, specs, opts)
}

func (c *countingRunner) PushBranch(ctx context.Context, branchName, remote string, opts git.PushOptions) error {
	c.pushBranch.Add(1)
	return c.Runner.PushBranch(ctx, branchName, remote, opts)
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

func TestSubmitCreatesNativeGitHubStackWhenRequested(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true, CreateGitHubStack: true}, &noopHandler{})
	require.NoError(t, err)
	require.Len(t, mockConfig.CreatedStacks, 1)

	basePR, err := s.Engine.GetBranch("base").GetPrInfo()
	require.NoError(t, err)
	apiPR, err := s.Engine.GetBranch("api").GetPrInfo()
	require.NoError(t, err)
	require.Equal(t, []int{*basePR.Number(), *apiPR.Number()}, mockConfig.CreatedStacks[0])

	// A repeated submit must leave the existing native Stack intact rather than
	// trying to create a second resource for the same pull requests.
	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true, CreateGitHubStack: true}, &noopHandler{})
	require.NoError(t, err)
	require.Len(t, mockConfig.CreatedStacks, 1)
}

func TestSubmitCreatesNativeGitHubStackWhenConfigured(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	require.NoError(t, cfg.SetGitHubStack(true))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{})
	require.NoError(t, err)
	require.Len(t, mockConfig.CreatedStacks, 1)
}

func TestSubmitConfiguredGitHubStackFallsBackWhenValidationPrunesToOnePullRequest(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})
	s.Checkout("base").Commit("advance base").Checkout("api")
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	require.NoError(t, cfg.SetGitHubStack(true))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{})
	require.NoError(t, err)
	require.Len(t, mockConfig.CreatedPRs, 1, "the valid parent should submit normally")
	require.Empty(t, mockConfig.CreatedStacks, "one surviving PR must not create native Stack metadata")
}

func TestSubmitConfiguredGitHubStackSkipsSinglePullRequest(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"feature": "main"})
	s.Checkout("feature")

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetGitHubStack(true))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{DryRun: true, ConfigGitHubStack: cfg.GitHubStack()}, &noopHandler{})
	require.NoError(t, err)
}

func TestSubmitConfiguredGitHubStackSkipsOversizedSubmission(t *testing.T) {
	t.Parallel()
	branches := make(map[string]string, github.MaxStackPullRequests+1)
	parent := "main"
	for i := range github.MaxStackPullRequests + 1 {
		branch := fmt.Sprintf("branch-%03d", i)
		branches[branch] = parent
		parent = branch
	}
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithStack(branches)
	s.Checkout(parent)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	require.NoError(t, cfg.SetGitHubStack(true))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{DryRun: true}, &noopHandler{})
	require.NoError(t, err, "an oversized submission is ineligible for native Stack sync but should still submit normally")
}

func TestSubmitGitHubStackRejectsMoreThanOneHundredBranchesBeforeSubmit(t *testing.T) {
	t.Parallel()
	branches := make(map[string]string, github.MaxStackPullRequests+1)
	parent := "main"
	for i := range github.MaxStackPullRequests + 1 {
		branch := fmt.Sprintf("branch-%03d", i)
		branches[branch] = parent
		parent = branch
	}
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithStack(branches)
	s.Checkout(parent)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{CreateGitHubStack: true}, &noopHandler{})
	require.EqualError(t, err, "a GitHub Stack supports at most 100 pull requests (got 101)")
}

func TestSubmitNativeGitHubStackRejectsPrunedSinglePullRequestBeforeSubmit(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})
	// Advance the parent after its child was created so submit validation prunes
	// the child for needing a restack.
	s.Checkout("base").Commit("advance base").Checkout("api")

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)
	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{NoEdit: true, CreateGitHubStack: true}, &noopHandler{})
	require.EqualError(t, err, "a GitHub Stack requires at least two pull requests")
	require.Empty(t, mockConfig.CreatedPRs, "native Stack validation must fail before submitting the surviving parent")
}

func TestSubmitGitHubStackRequiresLinearMode(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})

	err := submit.Action(s.Context, submit.Options{CreateGitHubStack: true}, &noopHandler{})
	require.EqualError(t, err, "native GitHub Stack creation requires stack.shape=linear; run 'stackit config set stack.shape linear'")
}

func TestSubmitGitHubStackRejectsForkedChain(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base", "ui": "base"})
	s.Checkout("base")

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{StackRange: engine.StackRangeFull(), CreateGitHubStack: true}, &noopHandler{})
	require.EqualError(t, err, "branch \"ui\" is stacked on \"base\", not on \"api\"; native GitHub Stacks require a single linear chain")
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

func TestSubmitDryRunCreateOnlySkipsRemoteStatusRead(t *testing.T) {
	t.Parallel()
	// A create-only dry run can compute every action locally; it should not pay a
	// remote ls-remote round trip when no push will happen.
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"P":  "main",
			"C1": "P",
			"C2": "C1",
		})

	s.Checkout("P")

	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	counting := &countingRunner{Runner: git.NewRunnerWithPath(s.Scene.Dir, nil)}
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
		Git:      counting,
	})
	require.NoError(t, err)

	ctx := app.NewContext(eng,
		app.WithRepoRoot(s.Scene.Dir),
		app.WithWriter(&bytes.Buffer{}),
		app.WithGlobalOptions(app.GlobalOptions{Verify: true}),
	)

	opts := submit.Options{
		StackRange: engine.StackRangeFull(),
		NoEdit:     true,
		DryRun:     true,
	}

	require.NoError(t, submit.Action(ctx, opts, &noopHandler{}))
	require.Equal(t, int64(0), counting.fetchRemoteShas.Load(),
		"create-only dry runs must not read remote status")
	require.Equal(t, int64(0), counting.pushBranches.Load(), "dry runs must not push")
	require.Equal(t, int64(0), counting.pushBranch.Load(), "dry runs must not push")
}

func TestSubmitPushesStackInSingleBatch(t *testing.T) {
	t.Parallel()
	// Regression test for the per-branch push N+1: submitting a stack must push
	// every branch in one `git push` invocation, not one push per branch.
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"P":  "main",
			"C1": "P",
			"C2": "C1",
		})

	s.Checkout("P")

	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

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

	require.Equal(t, int64(1), counting.pushBranches.Load(),
		"submit must push the whole stack in a single batched push")
	require.Equal(t, int64(0), counting.pushBranch.Load(),
		"submit must not fall back to per-branch pushes")

	// Every branch landed on the remote.
	for _, branch := range []string{"P", "C1", "C2"} {
		require.NoError(t, s.Scene.Repo.RunGitCommand("rev-parse", "--verify", "refs/remotes/origin/"+branch),
			"branch %s should be pushed to origin", branch)
	}
}

func TestSubmitNoOpReadsRemoteOnceAndSkipsPush(t *testing.T) {
	t.Parallel()
	// A second submit of an unchanged, in-sync stack must do no pushes and read
	// the remote ref list exactly once (the shared prefetch), not per branch.
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"P":  "main",
			"C1": "P",
			"C2": "C1",
		})

	s.Checkout("P")

	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

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

	// First submit: creates the PRs and pushes the branches. Use non-draft so a
	// subsequent submit with the same options has genuinely nothing to do.
	firstOpts := submit.Options{StackRange: engine.StackRangeFull(), NoEdit: true}
	require.NoError(t, submit.Action(ctx, firstOpts, &noopHandler{}))
	require.Equal(t, 3, len(config.CreatedPRs))

	// Second submit: nothing changed, so it should be a no-op.
	counting.fetchRemoteShas.Store(0)
	counting.pushBranches.Store(0)
	counting.pushBranch.Store(0)
	createdBefore := len(config.CreatedPRs)

	require.NoError(t, submit.Action(ctx, firstOpts, &noopHandler{}))

	require.Equal(t, createdBefore, len(config.CreatedPRs), "no-op submit must not create PRs")
	require.Equal(t, int64(0), counting.pushBranches.Load(), "no-op submit must not push")
	require.Equal(t, int64(0), counting.pushBranch.Load(), "no-op submit must not push")
	require.Equal(t, int64(1), counting.fetchRemoteShas.Load(),
		"no-op submit must read the remote ref list exactly once")
}

func TestSubmitPreservesLockStatus(t *testing.T) {
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
	t.Parallel()
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
	t.Parallel()
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
	childrenMap := make(map[string][]string)
	for _, branchName := range stack.Branches {
		parentName := stack.ParentMap[branchName]
		if parentName != "" {
			childrenMap[parentName] = append(childrenMap[parentName], branchName)
		}
	}
	require.Equal(t, []string{"feature"}, childrenMap["main"], "feature should appear as trunk child in display tree")
	require.Empty(t, childrenMap["wt-anchor"], "worktree anchor should not appear in display tree relationships")

	_, hasAnchorFixedState := stack.FixedMap["wt-anchor"]
	require.False(t, hasAnchorFixedState, "fixed map should not include non-submittable worktree anchors")
}

// captureSkipHandler records native GitHub Stack skip reasons and the final outcome.
type captureSkipHandler struct {
	reasons []string
	outcome submit.CompletionOutcome
}

func (h *captureSkipHandler) OnEvent(e submit.Event) {
	switch ev := e.(type) {
	case submit.GitHubStackSkippedEvent:
		h.reasons = append(h.reasons, ev.Reason)
	case submit.CompletionEvent:
		h.outcome = ev.Outcome
	}
}
func (h *captureSkipHandler) Confirm(_ string, defaultYes bool) (bool, error) { return defaultYes, nil }
func (h *captureSkipHandler) IsInteractive() bool                             { return false }

// Configured sync is best-effort: a team whose .stackit.yaml enables github.stack
// without stack.shape=linear must still be able to submit. Only the explicit
// --with-native-stack flag hard-fails (TestSubmitGitHubStackRequiresLinearMode).
func TestSubmitConfiguredGitHubStackSkipsNonLinearShape(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetGitHubStack(true)) // stack.shape stays the default "tree"
	s.Context.Config = cfg

	s.Checkout("api")
	handler := &captureSkipHandler{}
	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true}, handler)
	require.NoError(t, err, "configured sync must not fail a submit on a tree-shaped stack")
	require.Len(t, mockConfig.CreatedPRs, 2, "both PRs should still submit")
	require.Empty(t, mockConfig.CreatedStacks, "native Stack metadata must be skipped")
	require.Equal(t, submit.OutcomeComplete, handler.outcome)
	require.Contains(t, handler.reasons, "native GitHub Stack creation requires stack.shape=linear; run 'stackit config set stack.shape linear'")
}

// A forked chain is likewise a skip, not a failure, when sync came from config.
func TestSubmitConfiguredGitHubStackSkipsForkedChain(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base", "ui": "base"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	require.NoError(t, cfg.SetGitHubStack(true))
	s.Context.Config = cfg

	s.Checkout("base")
	handler := &captureSkipHandler{}
	err = submit.Action(s.Context, submit.Options{StackRange: engine.StackRangeFull(), NoEdit: true, Draft: true}, handler)
	require.NoError(t, err, "configured sync must not fail a submit on a pre-existing fork")
	require.Empty(t, mockConfig.CreatedStacks)
	require.Len(t, handler.reasons, 1)
	require.Contains(t, handler.reasons[0], `rooted at "base"`)
	require.Contains(t, handler.reasons[0], "branch \"ui\" is stacked on \"base\", not on \"api\"; native GitHub Stacks require a single linear chain")
}

// A repo without access to the experimental Stacks API must not turn a submit
// whose PRs all landed into a non-zero exit.
func TestSubmitConfiguredGitHubStackWarnsWhenStacksAPIFails(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	mockConfig.StackError = fmt.Errorf("stacks API unavailable")
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	require.NoError(t, cfg.SetGitHubStack(true))
	s.Context.Config = cfg

	s.Checkout("api")
	handler := &captureSkipHandler{}
	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true}, handler)
	require.NoError(t, err, "PRs landed, so the run must not report failure")
	require.Len(t, mockConfig.CreatedPRs, 2)
	require.Equal(t, submit.OutcomeComplete, handler.outcome)
	require.Len(t, handler.reasons, 1)
	require.Contains(t, handler.reasons[0], `rooted at "base"`)
	require.Contains(t, handler.reasons[0], "stacks API unavailable")
}

// The explicit flag keeps the strict contract: a Stacks API failure fails the run.
func TestSubmitExplicitGitHubStackFailsWhenStacksAPIFails(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	mockConfig.StackError = fmt.Errorf("stacks API unavailable")
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	s.Context.Config = cfg

	s.Checkout("api")
	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true, CreateGitHubStack: true}, &noopHandler{})
	require.ErrorContains(t, err, "creating native GitHub Stack metadata failed")
}

// `submit --stack` from trunk selects every branch in the repository. Each
// independent linear chain must be synchronized as its own native GitHub
// Stack, rather than being combined into one invalid request.
func TestSubmitGitHubStackSyncsDisjointChainsFromTrunk(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"one-bottom": "main",
			"one-top":    "one-bottom",
			"two-bottom": "main",
			"two-top":    "two-bottom",
		})
	s.Checkout("main")
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{StackRange: engine.StackRangeFull(), NoEdit: true, Draft: true, CreateGitHubStack: true}, &noopHandler{})
	require.NoError(t, err)
	require.Len(t, mockConfig.CreatedStacks, 2)

	oneBottomPR, err := s.Engine.GetBranch("one-bottom").GetPrInfo()
	require.NoError(t, err)
	oneTopPR, err := s.Engine.GetBranch("one-top").GetPrInfo()
	require.NoError(t, err)
	twoBottomPR, err := s.Engine.GetBranch("two-bottom").GetPrInfo()
	require.NoError(t, err)
	twoTopPR, err := s.Engine.GetBranch("two-top").GetPrInfo()
	require.NoError(t, err)
	require.Equal(t, [][]int{
		{*oneBottomPR.Number(), *oneTopPR.Number()},
		{*twoBottomPR.Number(), *twoTopPR.Number()},
	}, mockConfig.CreatedStacks)
}

func TestSubmitConfiguredGitHubStackSyncsDisjointChainsFromTrunk(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"one-bottom": "main",
			"one-top":    "one-bottom",
			"two-bottom": "main",
			"two-top":    "two-bottom",
		})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	require.NoError(t, cfg.SetGitHubStack(true))
	s.Context.Config = cfg

	s.Checkout("main")
	handler := &captureSkipHandler{}
	err = submit.Action(s.Context, submit.Options{StackRange: engine.StackRangeFull(), NoEdit: true, Draft: true}, handler)
	require.NoError(t, err, "submitting every stack must still succeed")
	require.Len(t, mockConfig.CreatedStacks, 2)
	require.Empty(t, handler.reasons, "independent linear chains should sync without a warning")
}

func TestSubmitConfiguredGitHubStackSyncsEligibleChainsAlongsideForks(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"linear-bottom": "main",
			"linear-top":    "linear-bottom",
			"fork-base":     "main",
			"fork-one":      "fork-base",
			"fork-two":      "fork-base",
		})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	require.NoError(t, cfg.SetGitHubStack(true))
	s.Context.Config = cfg

	s.Checkout("main")
	handler := &captureSkipHandler{}
	err = submit.Action(s.Context, submit.Options{StackRange: engine.StackRangeFull(), NoEdit: true, Draft: true}, handler)
	require.NoError(t, err)
	require.Len(t, mockConfig.CreatedStacks, 1, "the valid chain should still synchronize")
	require.Len(t, handler.reasons, 1, "the fork should be skipped independently")
	require.Contains(t, handler.reasons[0], `rooted at "fork-base"`)
	require.Contains(t, handler.reasons[0], `branch "fork-two" is stacked on "fork-base", not on "fork-one"`)
}

// A genuine single chain still syncs.
func TestSubmitGitHubStackAcceptsContiguousChain(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"bottom": "main", "middle": "bottom", "top": "middle"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	s.Context.Config = cfg

	s.Checkout("top")
	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true, CreateGitHubStack: true}, &noopHandler{})
	require.NoError(t, err)
	require.Len(t, mockConfig.CreatedStacks, 1)
	require.Len(t, mockConfig.CreatedStacks[0], 3, "all three PRs should form one native Stack")
}

// TestSubmitDissolvesNativeGitHubStackToRetargetBase covers reparenting a branch
// out of a submitted native GitHub Stack (`stackit move` onto trunk). GitHub
// refuses to change a stacked PR's base, so submit must dissolve the stale
// Stack and retry rather than failing every time from then on.
func TestSubmitDissolvesNativeGitHubStackToRetargetBase(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true, CreateGitHubStack: true}, &noopHandler{})
	require.NoError(t, err)
	require.Len(t, mockConfig.CreatedStacks, 1)

	// Move "api" out of the stack onto trunk, then resubmit just that branch —
	// too few PRs for a native Stack, so nothing else clears the stale one.
	s.TrackBranch("api", "main").Checkout("api")
	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{})
	require.NoError(t, err)

	require.Empty(t, mockConfig.CreatedStacks[0], "the stale native Stack should have been dissolved")
	apiPR, err := s.Engine.GetBranch("api").GetPrInfo()
	require.NoError(t, err)
	require.Equal(t, "main", apiPR.Base())
}

// TestSubmitRetargetsBaseWhenStackKeepsMergedPullRequest covers the state a
// stack lands in as soon as one of its members merges: GitHub refuses to
// unstack a merged pull request, so the Stack never dissolves outright. Treating
// that as a failed dissolve left the base retarget blocked and surfaced GitHub's
// raw 422, even though the pull request being retargeted had in fact left the
// Stack — and a later submit would then succeed, making it look intermittent.
func TestSubmitRetargetsBaseWhenStackKeepsMergedPullRequest(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"base": "main", "api": "base"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	mockConfig := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

	cfg, err := stackitconfig.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetStackShape(stackitconfig.StackShapeLinear))
	s.Context.Config = cfg

	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true, CreateGitHubStack: true}, &noopHandler{})
	require.NoError(t, err)
	require.Len(t, mockConfig.CreatedStacks, 1)
	require.NotEmpty(t, mockConfig.CreatedStacks[0])

	// The bottom pull request merges. GitHub keeps it in the Stack and will not
	// unstack it, so the Stack can never be emptied from here on.
	mockConfig.MergedStackPRs = []int{mockConfig.CreatedStacks[0][0]}

	s.TrackBranch("api", "main").Checkout("api")
	err = submit.Action(s.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{})
	require.NoError(t, err, "a Stack pinned open by a merged PR must not block retargeting the others")

	apiPR, err := s.Engine.GetBranch("api").GetPrInfo()
	require.NoError(t, err)
	require.Equal(t, "main", apiPR.Base())

	require.Equal(t, mockConfig.MergedStackPRs, mockConfig.CreatedStacks[0],
		"only the merged pull request should remain stacked")
}
