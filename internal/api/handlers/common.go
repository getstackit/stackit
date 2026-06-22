package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/utils"
)

// defaultRepoID is the ID assigned to the single bootstrap repo when the
// server is started with `-cwd` (single-repo legacy shortcut). It's also
// substituted on the unscoped legacy routes that don't carry {repoID}.
const defaultRepoID = "default"

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

// resolveRepo looks up the repo entry for the {repoID} path value on r.
// If the path value is empty (legacy/unscoped route or direct test call)
// it falls back to defaultRepoID. Returns false and writes a 404 when the
// repoID does not exist in the registry.
func resolveRepo(reg *registry.Registry, w http.ResponseWriter, r *http.Request) (*registry.RepoEntry, bool) {
	id := r.PathValue("repoID")
	if id == "" {
		id = defaultRepoID
	}
	entry, ok := reg.Get(id)
	if !ok {
		http.NotFound(w, r)
		return nil, false
	}
	return entry, true
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
