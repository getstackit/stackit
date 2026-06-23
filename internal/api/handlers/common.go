package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/api/auth"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/utils"
)

// Visibility controls whether a handler may expose operator- or
// viewer-identifying fields in its response. A public (read-only) server
// must not leak who is running it, so identity fields like currentUser are
// omitted under VisibilityPublic.
type Visibility int

const (
	// VisibilityPrivate is the authenticated/local posture: responses may
	// include the operator/viewer identity.
	VisibilityPrivate Visibility = iota
	// VisibilityPublic is the anonymous read-only posture: identity fields
	// are omitted so an unauthenticated caller can't learn who runs the
	// server (and so reads don't trigger GitHub calls on the operator's
	// token).
	VisibilityPublic
)

// resolveRepo looks up the repo entry for the {owner}/{repo} path values on r,
// matched case-insensitively against the registry's GitHub coordinates. Returns
// false and writes a 404 when no such repo exists or it is not visible to the
// requesting user. A repo invisible to the caller 404s (not 403) so its
// existence is not disclosed.
func resolveRepo(reg *registry.Registry, w http.ResponseWriter, r *http.Request) (*registry.RepoEntry, bool) {
	entry, ok := reg.GetByOwnerRepo(r.PathValue("owner"), r.PathValue("repo"))
	if !ok || !visibleTo(entry, requestUser(r)) {
		http.NotFound(w, r)
		return nil, false
	}
	return entry, true
}

// requestUser returns the authenticated GitHub login for r, or "" when the
// request is anonymous — read-only or auth-disabled servers, which carry no
// session and are deliberately open.
func requestUser(r *http.Request) string {
	if sess := auth.SessionFromContext(r.Context()); sess != nil {
		return sess.GitHubLogin
	}
	return ""
}

// visibleTo reports whether entry should be visible to user. A repo with no
// recorded adder (operator-seeded) is shared with everyone; a repo onboarded by
// a user is visible only to that user. Anonymous readers have no identity to
// match, so they see only shared repos.
func visibleTo(entry *registry.RepoEntry, user string) bool {
	if entry.AddedBy == "" {
		return true
	}
	return user != "" && entry.AddedBy == user
}

// validateBranchName runs branch name through the same validator the CLI
// uses (utils.ValidateBranchName) before any handler hands it to git. The
// error message intentionally does not include the user-supplied name so
// the response can't reflect arbitrary content back to a caller.
func validateBranchName(w http.ResponseWriter, name string) bool {
	if err := utils.ValidateBranchName(name); err != nil {
		http.Error(w, "invalid branch name", http.StatusBadRequest)
		return false
	}
	return true
}
