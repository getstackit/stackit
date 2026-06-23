package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

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

func (f *fakeGitHub) GetOwnerRepo() (string, string) { return f.owner, f.repo }

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
