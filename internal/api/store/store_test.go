package store_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/store"
	"github.com/getstackit/stackit/internal/api/store/testpostgres"
)

var (
	harnessOnce sync.Once
	harness     *testpostgres.Harness
	harnessErr  error
)

// TestMain lazily provisions a shared Postgres backend (the first acquire call
// starts it) and terminates a container-backed harness once all tests finish.
func TestMain(m *testing.M) {
	code := m.Run()
	if harness != nil {
		_ = harness.Terminate(context.Background())
	}
	os.Exit(code)
}

// acquire returns a connection string for an empty, migratable schema, or
// skips the test when no Postgres backend is available.
func acquire(t *testing.T) string {
	t.Helper()
	harnessOnce.Do(func() {
		harness, harnessErr = testpostgres.Start(context.Background())
	})
	if errors.Is(harnessErr, testpostgres.ErrUnavailable) {
		t.Skip("no postgres backend: set STACKIT_TEST_DATABASE_URL or start Docker")
	}
	require.NoError(t, harnessErr)
	harness.Reset(t)
	return harness.URL
}

func sampleRepo() store.Repo {
	return store.Repo{
		ID:          "acme-widgets",
		DisplayName: "acme/widgets",
		Owner:       "acme",
		Name:        "widgets",
		Path:        "acme/widgets",
		Remote:      "origin",
	}
}

func TestStore_OpenRunsMigrationsAndListsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := store.Open(ctx, acquire(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	repos, err := st.ListRepos(ctx)
	require.NoError(t, err)
	require.Empty(t, repos)
}

func TestStore_AddGetListDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := store.Open(ctx, acquire(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	added, err := st.AddRepo(ctx, sampleRepo())
	require.NoError(t, err)
	require.Equal(t, "acme-widgets", added.ID)
	require.False(t, added.CreatedAt.IsZero(), "server should assign created_at")
	require.False(t, added.UpdatedAt.IsZero(), "server should assign updated_at")

	got, err := st.GetRepo(ctx, "acme-widgets")
	require.NoError(t, err)
	require.Equal(t, "acme", got.Owner)
	require.Equal(t, "widgets", got.Name)
	require.Equal(t, "origin", got.Remote)

	repos, err := st.ListRepos(ctx)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, "acme-widgets", repos[0].ID)

	require.NoError(t, st.DeleteRepo(ctx, "acme-widgets"))

	_, err = st.GetRepo(ctx, "acme-widgets")
	require.ErrorIs(t, err, store.ErrRepoNotFound)

	repos, err = st.ListRepos(ctx)
	require.NoError(t, err)
	require.Empty(t, repos)
}

func TestStore_GetMissingReturnsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := store.Open(ctx, acquire(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	_, err = st.GetRepo(ctx, "nope")
	require.ErrorIs(t, err, store.ErrRepoNotFound)
}

func TestStore_AddDuplicateIDFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := store.Open(ctx, acquire(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	_, err = st.AddRepo(ctx, sampleRepo())
	require.NoError(t, err)

	_, err = st.AddRepo(ctx, sampleRepo())
	require.Error(t, err, "duplicate primary key should fail")
}

func TestStore_DeleteMissingIsNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := store.Open(ctx, acquire(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	require.NoError(t, st.DeleteRepo(ctx, "nope"))
}

func TestStore_OpenEmptyConnStringErrors(t *testing.T) {
	t.Parallel()
	_, err := store.Open(context.Background(), "  ")
	require.Error(t, err)
}
