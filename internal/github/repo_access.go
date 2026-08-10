package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/go-github/v90/github"
)

// ErrRepoAccessDenied is returned by CheckRepoAccess when the authenticated
// user cannot see the repository. GitHub deliberately returns 404 (not 403)
// for private repos a token can't read, so "not found" and "no access" are
// indistinguishable and collapse into this one error.
var ErrRepoAccessDenied = errors.New("github: repository not found or access denied")

// RepoAccess describes a user's access to a GitHub repository — enough for the
// onboarding flow to decide whether to proceed and how to clone it. Because the
// lookup is authenticated with a specific user's token, CanPush reflects that
// user's permission level, not the server's.
type RepoAccess struct {
	Owner         string
	Name          string
	DefaultBranch string
	CloneURL      string
	Private       bool
	CanPush       bool
}

// CheckRepoAccess verifies that token grants access to owner/name on github.com
// and returns the repository's canonical coordinates. A nil error means the
// authenticated user can at least read the repo; ErrRepoAccessDenied means the
// repo is missing or invisible to that token. Other errors are transport-level.
func CheckRepoAccess(ctx context.Context, token, owner, name string) (*RepoAccess, error) {
	client, err := createGitHubClient(ctx, "github.com", token)
	if err != nil {
		return nil, err
	}
	return repoAccessFromClient(ctx, client, owner, name)
}

// repoAccessFromClient performs the lookup against an already-built client so
// tests can point client.BaseURL at a stub server.
func repoAccessFromClient(ctx context.Context, client *github.Client, owner, name string) (*RepoAccess, error) {
	repo, resp, err := client.Repositories.Get(ctx, owner, name)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden) {
			return nil, ErrRepoAccessDenied
		}
		return nil, fmt.Errorf("get repo %s/%s: %w", owner, name, err)
	}

	access := &RepoAccess{
		Owner:         repo.GetOwner().GetLogin(),
		Name:          repo.GetName(),
		DefaultBranch: repo.GetDefaultBranch(),
		CloneURL:      repo.GetCloneURL(),
		Private:       repo.GetPrivate(),
	}
	if perms := repo.GetPermissions(); perms != nil {
		access.CanPush = perms.GetPush()
	}
	return access, nil
}
