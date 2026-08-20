package handlers

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// fakeGitHub implements only the github.Client methods buildRepo touches.
// The embedded interface is nil, so any other call panics — which keeps the
// fake honest about what the code under test actually depends on.
type fakeGitHub struct {
	github.Client
	owner       string
	repo        string
	currentUser string
}

func (f *fakeGitHub) Repo() github.Repo { return github.Repo{Owner: f.owner, Name: f.repo} }

func (f *fakeGitHub) GetCurrentUser(context.Context) (string, error) {
	return f.currentUser, nil
}

func TestBuildRepoOmitsCurrentUserWhenPublic(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	gh := &fakeGitHub{owner: "acme", repo: "widgets", currentUser: "operator-login"}

	t.Run("private includes the operator identity", func(t *testing.T) {
		t.Parallel()
		a := NewViewAssembler(s.Engine, gh, "origin", VisibilityPrivate)
		repo := a.buildRepo(context.Background())
		require.Equal(t, "operator-login", repo.CurrentUser)
		require.Equal(t, "acme", repo.Owner)
		require.Equal(t, "widgets", repo.Repo)
	})

	t.Run("public omits the operator identity", func(t *testing.T) {
		t.Parallel()
		a := NewViewAssembler(s.Engine, gh, "origin", VisibilityPublic)
		repo := a.buildRepo(context.Background())
		require.Empty(t, repo.CurrentUser, "a public read-only server must not leak who runs it")
		// Repo coordinates are public information (they match the PRs on
		// GitHub) and stay populated.
		require.Equal(t, "acme", repo.Owner)
		require.Equal(t, "widgets", repo.Repo)
	})
}

// TestBuildSurvivesDetachedHEAD reproduces the server-mirror crash: managed
// repo checkouts run with a detached HEAD, so engine.CurrentBranch() is nil.
// The whole /view assembly path (Build -> MapStackDetail -> MapStackSummary)
// must read the current branch nil-safely rather than dereference it.
func TestBuildSurvivesDetachedHEAD(t *testing.T) {
	t.Parallel()

	// A linear stack so the mapper actually runs MapStackSummary per stack —
	// without one, the loop is empty and the panic site is never reached.
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3()

	head, err := s.Scene.Repo.GetRevision("HEAD")
	require.NoError(t, err)
	require.NoError(t, s.Scene.Repo.CheckoutDetached(head))
	require.Nil(t, s.Engine.CurrentBranch(), "scenario must be in detached HEAD")

	// gh is nil: the anonymous public read path with no GitHub calls, mirroring
	// the request that panicked.
	a := NewViewAssembler(s.Engine, nil, "origin", VisibilityPublic)
	view, err := a.Build(context.Background())
	require.NoError(t, err)
	require.Empty(t, view.Repo.CurrentBranch, "detached HEAD has no current branch")
	require.NotEmpty(t, view.Stacks, "the stack should still be mapped while detached")
}

// countingFetchRunner wraps a git.Runner and counts FetchRemoteShas calls, so
// a test can assert /view reads remote status once for the whole response
// rather than once per stack. FetchRemoteShas runs `git ls-remote`, a network
// round trip.
type countingFetchRunner struct {
	git.Runner
	fetchRemoteShas atomic.Int64
}

func (c *countingFetchRunner) FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	c.fetchRemoteShas.Add(1)
	return c.Runner.FetchRemoteShas(ctx, remote)
}

// TestBuildReadsRemoteStatusOnceAcrossStacks reproduces the /view N+1: two
// independent stacks off trunk must still cost one remote listing, not one
// per stack.
func TestBuildReadsRemoteStatusOnceAcrossStacks(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"stack-a": "main", "stack-b": "main"})

	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)
	for _, b := range []string{"main", "stack-a", "stack-b"} {
		require.NoError(t, s.Scene.Repo.PushBranch("origin", b))
	}

	counting := &countingFetchRunner{Runner: git.NewRunnerWithPath(s.Scene.Dir, nil)}
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
		Git:      counting,
	})
	require.NoError(t, err)

	a := NewViewAssembler(eng, nil, "origin", VisibilityPublic)
	view, err := a.Build(context.Background())
	require.NoError(t, err)
	require.Len(t, view.Stacks, 2, "stack-a and stack-b are independent stacks off trunk")

	require.Equal(t, int64(1), counting.fetchRemoteShas.Load(),
		"building /view across multiple stacks must read remote status once, not once per stack")
}
