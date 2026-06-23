package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/registry"
)

type fakeManagedSyncer struct {
	calls []string
	err   error
}

func (f *fakeManagedSyncer) SyncRepo(_ context.Context, owner, name string) error {
	f.calls = append(f.calls, owner+"/"+name)
	return f.err
}

func syncRequest(repoID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos/"+repoID+"/sync", nil)
	req.SetPathValue("repoID", repoID)
	return req
}

func TestSyncHandler_ManagedRepoMirrorFetches(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "m", Managed: true, Owner: "octo", Name: "widget"}))

	sy := &fakeManagedSyncer{}
	h := NewSyncHandler(reg, sy)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, syncRequest("m"))

	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Equal(t, []string{"octo/widget"}, sy.calls)
}

func TestSyncHandler_ManagedRepoFetchErrorReturns502(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "m", Managed: true, Owner: "octo", Name: "widget"}))

	sy := &fakeManagedSyncer{err: errors.New("fetch boom")}
	h := NewSyncHandler(reg, sy)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, syncRequest("m"))

	require.Equal(t, http.StatusBadGateway, rr.Code)
}

func TestSyncHandler_LocalRepoRefreshesWithoutFetch(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	// Unmanaged repo with no engine: Refresh is a no-op, but the key assertion
	// is that the managed mirror-fetch path is never taken for a working repo
	// (which would detach its HEAD).
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "default", Managed: false}))

	sy := &fakeManagedSyncer{}
	h := NewSyncHandler(reg, sy)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, syncRequest("default"))

	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Empty(t, sy.calls, "a local working repo must never be mirror-fetched")
}

func TestSyncHandler_UnknownRepo404(t *testing.T) {
	t.Parallel()
	h := NewSyncHandler(registry.New(), &fakeManagedSyncer{})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, syncRequest("nope"))

	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestSyncHandler_RejectsNonPost(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "m", Managed: true, Owner: "o", Name: "n"}))
	h := NewSyncHandler(reg, &fakeManagedSyncer{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/m/sync", nil)
	req.SetPathValue("repoID", "m")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}
