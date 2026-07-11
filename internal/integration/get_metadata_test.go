package integration

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/go-github/v88/github"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/submit"
	syncaction "github.com/getstackit/stackit/internal/actions/sync"
	githubpkg "github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// countingGitHubClient wraps a github.Client and counts GetPullRequestByBranch
// calls, so tests can assert whether get fell back to the GitHub ancestry crawl.
type countingGitHubClient struct {
	githubpkg.Client
	prByBranch atomic.Int64
}

func (c *countingGitHubClient) GetPullRequestByBranch(ctx context.Context, branch string) (*githubpkg.PullRequestInfo, error) {
	c.prByBranch.Add(1)
	return c.Client.GetPullRequestByBranch(ctx, branch)
}

// recordingGetHandler captures the PR number surfaced for each branch. EmitEvent is
// only called from sequential phases (sync loop, restack callback), so no locking.
type recordingGetHandler struct {
	actions.GetNullHandler
	prByBranch map[string]int
}

func (h *recordingGetHandler) EmitEvent(e actions.GetEvent) {
	if e.Branch == "" || e.PRNumber == nil {
		return
	}
	if h.prByBranch == nil {
		h.prByBranch = map[string]int{}
	}
	h.prByBranch[e.Branch] = *e.PRNumber
}

// dropLocalBranch removes a branch and its metadata locally, simulating a branch the
// user does not have (forcing get's recreate path). The remote copy is untouched.
func dropLocalBranch(t *testing.T, sh *scenario.Scenario, branch string) {
	t.Helper()
	require.NoError(t, sh.Scene.Repo.RunGitCommand("branch", "-D", branch))
	_ = sh.Scene.Repo.RunGitCommand("update-ref", "-d", "refs/stackit/metadata/"+branch)
	_ = sh.Scene.Repo.RunGitCommand("update-ref", "-d", "refs/stackit/local-metadata/"+branch)
}

// dropRemoteMetadata removes the pushed metadata ref and the local fetched cache for
// a branch, simulating a partial remote metadata chain.
func dropRemoteMetadata(t *testing.T, sh *scenario.Scenario, branch string) {
	t.Helper()
	require.NoError(t, sh.Scene.Repo.RunGitCommand("push", "origin", ":refs/stackit/metadata/"+branch))
	_ = sh.Scene.Repo.RunGitCommand("update-ref", "-d", "refs/stackit/remote-metadata/"+branch)
}

func newCountingGitHubClient(t *testing.T, config *testhelpers.MockGitHubServerConfig) *countingGitHubClient {
	t.Helper()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
	return &countingGitHubClient{Client: testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)}
}

func TestGetMetadataDiscovery(t *testing.T) {
	t.Parallel()

	t.Run("resolves parents and PR numbers from metadata without a GitHub crawl", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		// Build main -> a -> b as a stackit stack.
		sh.CreateBranch("a").CommitChange("a.txt", "a").TrackBranch("a", "main")
		sh.CreateBranch("b").CommitChange("b.txt", "b").TrackBranch("b", "a")

		config := testhelpers.NewMockGitHubServerConfig()
		gh := newCountingGitHubClient(t, config)
		sh.Context.GitHubClient = gh

		// Submit + sync so parent and PR info land in the pushed metadata.
		require.NoError(t, submit.Action(sh.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{}))
		require.NoError(t, syncaction.Action(sh.Context, syncaction.Options{}, nil))

		// Precondition: the remote metadata carries the PR number, so get has no reason
		// to call GitHub for display either.
		require.NoError(t, sh.Engine.LoadRemoteMetadataCache())
		cache := sh.Engine.GetRemoteMetadataCache()
		require.NotNil(t, cache.Get("b"), "remote metadata for b should exist")
		require.NotNil(t, cache.Get("b").GetParentBranchName(), "remote metadata for b should record its parent")
		bPR := cache.Get("b").GetPrInfo()
		require.NotNil(t, bPR, "remote metadata for b should carry PR info")
		require.NotNil(t, bPR.Number, "remote metadata for b should carry a PR number")

		// Forget the branches locally so get must recreate them from the remote.
		sh.Checkout("main")
		dropLocalBranch(t, sh, "b")
		dropLocalBranch(t, sh, "a")
		sh.Rebuild()

		// Now measure: get should resolve everything from metadata, zero GitHub crawl.
		gh.prByBranch.Store(0)
		handler := &recordingGetHandler{}
		require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: true}, handler))
		sh.Rebuild()

		sh.ExpectStackStructure(map[string]string{"b": "a", "a": "main"})
		require.Equal(t, int64(0), gh.prByBranch.Load(),
			"get should resolve parents and PR numbers from metadata without any GitHub PR-by-branch calls")
		require.Equal(t, *bPR.Number, handler.prByBranch["b"], "get should surface the PR number from metadata")
	})

	t.Run("falls back to GitHub when remote metadata is absent", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		// A raw git branch pushed to origin with NO stackit metadata.
		sh.CreateBranchQuiet("feature-x")
		sh.CommitChange("fx.txt", "feature x")
		require.NoError(t, sh.Scene.Repo.RunGitCommand("push", "-u", "origin", "feature-x"))
		sh.CheckoutQuiet("main")
		require.NoError(t, sh.Scene.Repo.RunGitCommand("branch", "-D", "feature-x"))
		sh.Rebuild()

		// GitHub knows about the PR (base main); metadata does not.
		config := testhelpers.NewMockGitHubServerConfig()
		config.PRs["feature-x"] = &github.PullRequest{
			Number: new(42),
			Head:   &github.PullRequestBranch{Ref: new("feature-x")},
			Base:   &github.PullRequestBranch{Ref: new("main")},
			State:  new("open"),
		}
		gh := newCountingGitHubClient(t, config)
		sh.Context.GitHubClient = gh

		require.NoError(t, actions.GetAction(sh.Context, "feature-x", actions.GetOptions{Restack: true}, &actions.GetNullHandler{}))
		sh.Rebuild()

		sh.ExpectStackStructure(map[string]string{"feature-x": "main"})
		require.GreaterOrEqual(t, gh.prByBranch.Load(), int64(1),
			"get should fall back to the GitHub crawl when metadata is absent")
	})

	t.Run("falls back to GitHub when ancestor metadata is missing", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		sh.CreateBranch("a").CommitChange("a.txt", "a").TrackBranch("a", "main")
		sh.CreateBranch("b").CommitChange("b.txt", "b").TrackBranch("b", "a")

		config := testhelpers.NewMockGitHubServerConfig()
		gh := newCountingGitHubClient(t, config)
		sh.Context.GitHubClient = gh

		require.NoError(t, submit.Action(sh.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{}))
		require.NoError(t, syncaction.Action(sh.Context, syncaction.Options{}, nil))

		// Keep b's metadata pointing to a, but remove a's metadata. Metadata discovery
		// should not accept this partial chain; GitHub can still provide a -> main.
		dropRemoteMetadata(t, sh, "a")

		sh.Checkout("main")
		dropLocalBranch(t, sh, "b")
		dropLocalBranch(t, sh, "a")
		sh.Rebuild()

		gh.prByBranch.Store(0)
		require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: true}, &actions.GetNullHandler{}))
		sh.Rebuild()

		sh.ExpectStackStructure(map[string]string{"b": "a", "a": "main"})
		require.GreaterOrEqual(t, gh.prByBranch.Load(), int64(2),
			"get should crawl GitHub for the target and missing ancestor when metadata is partial")
	})

	t.Run("single branch resolves from metadata without a GitHub crawl", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		sh.CreateBranch("feature").CommitChange("f.txt", "f").TrackBranch("feature", "main")

		config := testhelpers.NewMockGitHubServerConfig()
		gh := newCountingGitHubClient(t, config)
		sh.Context.GitHubClient = gh

		require.NoError(t, submit.Action(sh.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{}))
		require.NoError(t, syncaction.Action(sh.Context, syncaction.Options{}, nil))

		sh.Checkout("main")
		dropLocalBranch(t, sh, "feature")
		sh.Rebuild()

		gh.prByBranch.Store(0)
		require.NoError(t, actions.GetAction(sh.Context, "feature", actions.GetOptions{Restack: true}, &actions.GetNullHandler{}))
		sh.Rebuild()

		sh.ExpectStackStructure(map[string]string{"feature": "main"})
		require.Equal(t, int64(0), gh.prByBranch.Load(),
			"single-branch get should resolve from metadata without any GitHub PR-by-branch calls")
	})
}
