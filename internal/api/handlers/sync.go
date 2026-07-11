package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/getstackit/stackit/internal/api/registry"
)

// ManagedSyncer mirror-fetches and refreshes a managed repo by its GitHub
// coordinates, synchronously. The concrete implementation is *reposync.Syncer.
// The manual-sync handler uses the synchronous form (not the coalescer) so it
// can report success or failure back to the caller.
type ManagedSyncer interface {
	SyncRepo(ctx context.Context, repo registry.RepoRef) error
}

// SyncHandler serves POST /api/v1/repos/{repoID}/sync: force an immediate
// refresh of one repo on demand. It is the manual complement to the webhook and
// interval refreshes, and the primary way to pull remote changes on a local
// server that GitHub cannot reach with a webhook.
//
// The refresh path depends on whether the repo is a server-managed mirror:
//   - Managed mirror: mirror-fetch from the remote, then rebuild. The checkout
//     runs detached, so fetching every branch is safe.
//   - Local -cwd working repo: only re-read the on-disk refs and rebuild. It is
//     never mirror-fetched, because that detaches HEAD and would corrupt the
//     operator's working tree (see safety-invariants.md). Pulling the remote on
//     a working repo is the human's job (git fetch / stackit sync); this just
//     reflects whatever is on disk right now.
type SyncHandler struct {
	reg    *registry.Registry
	syncer ManagedSyncer
}

// NewSyncHandler wires a manual-sync handler over the registry and syncer.
func NewSyncHandler(reg *registry.Registry, syncer ManagedSyncer) *SyncHandler {
	return &SyncHandler{reg: reg, syncer: syncer}
}

func (h *SyncHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entry, ok := resolveRepo(h.reg, w, r)
	if !ok {
		return
	}

	if entry.Managed {
		if err := h.syncer.SyncRepo(r.Context(), entry.RepoRef); err != nil {
			slog.Warn("manual sync failed", "repo", entry.ID, "error", err)
			http.Error(w, "sync failed", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Unmanaged (local) repo: re-read on-disk refs without fetching, so the
	// operator's working tree HEAD is never touched.
	entry.Refresh()
	w.WriteHeader(http.StatusNoContent)
}
