package reposync

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	token string
	err   error
}

func (f fakeProvider) InstallationToken(_ context.Context, _, _ string) (string, error) {
	return f.token, f.err
}

func managedEntry(id, owner, name, root string) *registry.RepoEntry {
	return &registry.RepoEntry{ID: id, Managed: true, RepoRef: registry.RepoRef{Owner: owner, Name: name}, RepoRoot: root, Remote: "origin"}
}

func TestSyncOnceFetchesManagedOnly(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(managedEntry("managed", "octo", "widget", "/r/m")))
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "unmanaged", RepoRef: registry.RepoRef{Owner: "octo", Name: "dev"}}))

	var mu sync.Mutex
	fetched := map[string]string{}
	refreshed := map[string]bool{}

	s := New(reg, fakeProvider{token: "inst-token"}, time.Minute)
	s.fetch = func(_ context.Context, repoRoot, _, token string) error {
		mu.Lock()
		defer mu.Unlock()
		fetched[repoRoot] = token
		return nil
	}
	s.refresh = func(e *registry.RepoEntry) {
		mu.Lock()
		defer mu.Unlock()
		refreshed[e.ID] = true
	}

	s.syncOnce(context.Background())

	require.Equal(t, map[string]string{"/r/m": "inst-token"}, fetched, "only the managed repo is fetched, with its installation token")
	require.Equal(t, map[string]bool{"managed": true}, refreshed)
}

func TestSyncEntrySkipsRefreshOnFetchError(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(managedEntry("m", "o", "n", "/r")))

	refreshed := false
	s := New(reg, fakeProvider{token: "t"}, time.Minute)
	s.fetch = func(context.Context, string, string, string) error { return errors.New("boom") }
	s.refresh = func(*registry.RepoEntry) { refreshed = true }

	s.syncOnce(context.Background())
	require.False(t, refreshed, "a failed fetch must not refresh stale state")
}

func TestSyncEntrySkipsOnTokenError(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(managedEntry("m", "o", "n", "/r")))

	fetchCalled := false
	s := New(reg, fakeProvider{err: errors.New("not installed")}, time.Minute)
	s.fetch = func(context.Context, string, string, string) error { fetchCalled = true; return nil }
	s.refresh = func(*registry.RepoEntry) {}

	s.syncOnce(context.Background())
	require.False(t, fetchCalled, "no token means no fetch")
}

func TestSyncEntryNilProviderUsesEmptyToken(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(managedEntry("m", "o", "n", "/r")))

	var gotToken string
	s := New(reg, nil, time.Minute)
	s.fetch = func(_ context.Context, _, _, token string) error { gotToken = token; return nil }
	s.refresh = func(*registry.RepoEntry) {}

	s.syncOnce(context.Background())
	require.Equal(t, "", gotToken, "no provider means an unauthenticated (public) fetch")
}

func TestSyncRepoFetchesMatchingManagedEntry(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(managedEntry("m", "Octo", "Widget", "/r/m")))

	var gotToken string
	refreshed := false
	s := New(reg, fakeProvider{token: "inst-token"}, time.Minute)
	s.fetch = func(_ context.Context, _, _, token string) error { gotToken = token; return nil }
	s.refresh = func(*registry.RepoEntry) { refreshed = true }

	// Casing differs from the entry to prove the lookup is case-insensitive,
	// matching how a webhook payload may present the coordinates.
	require.NoError(t, s.SyncRepo(context.Background(), registry.RepoRef{Owner: "octo", Name: "widget"}))
	require.Equal(t, "inst-token", gotToken)
	require.True(t, refreshed)
}

func TestSyncRepoUnknownRepoReturnsErrNotManaged(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(managedEntry("m", "o", "n", "/r")))

	fetchCalled := false
	s := New(reg, fakeProvider{token: "t"}, time.Minute)
	s.fetch = func(context.Context, string, string, string) error { fetchCalled = true; return nil }
	s.refresh = func(*registry.RepoEntry) {}

	err := s.SyncRepo(context.Background(), registry.RepoRef{Owner: "other", Name: "repo"})
	require.ErrorIs(t, err, ErrRepoNotManaged)
	require.False(t, fetchCalled, "an un-onboarded repo must not trigger a fetch")
}

func TestSyncRepoPropagatesFetchError(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(managedEntry("m", "o", "n", "/r")))

	refreshed := false
	s := New(reg, fakeProvider{token: "t"}, time.Minute)
	s.fetch = func(context.Context, string, string, string) error { return errors.New("boom") }
	s.refresh = func(*registry.RepoEntry) { refreshed = true }

	err := s.SyncRepo(context.Background(), registry.RepoRef{Owner: "o", Name: "n"})
	require.Error(t, err)
	require.False(t, refreshed, "a failed fetch must not refresh stale state")
}

func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()
	s := New(registry.New(), nil, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
