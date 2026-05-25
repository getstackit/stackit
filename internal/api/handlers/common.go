package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/api/registry"
)

// defaultRepoID is the ID assigned to the single bootstrap repo when the
// server is started with `-cwd` (single-repo legacy shortcut). It's also
// substituted on the unscoped legacy routes that don't carry {repoID}.
const defaultRepoID = "default"

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
