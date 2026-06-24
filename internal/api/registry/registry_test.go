package registry

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistry_AddValidatesID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entry   *RepoEntry
		wantErr string
	}{
		{name: "nil entry", entry: nil, wantErr: "nil entry"},
		{name: "empty ID", entry: &RepoEntry{}, wantErr: "empty ID"},
		{name: "slash in ID", entry: &RepoEntry{ID: "a/b"}, wantErr: "invalid repo ID"},
		{name: "space in ID", entry: &RepoEntry{ID: "a b"}, wantErr: "invalid repo ID"},
		{name: "dot in ID", entry: &RepoEntry{ID: "a.b"}, wantErr: "invalid repo ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := New()
			err := r.Add(tt.entry)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestRegistry_AddAllowsValidIDs(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"a", "stackit", "my-repo", "my_repo", "repo123", "ABC"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			r := New()
			require.NoError(t, r.Add(&RepoEntry{ID: id}))
			_, ok := r.Get(id)
			require.True(t, ok)
		})
	}
}

func TestRegistry_AddRejectsDuplicateID(t *testing.T) {
	t.Parallel()
	r := New()
	require.NoError(t, r.Add(&RepoEntry{ID: "dup"}))
	err := r.Add(&RepoEntry{ID: "dup"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate repo ID")
}

func TestRegistry_AddRejectsDuplicateOwnerRepo(t *testing.T) {
	t.Parallel()
	r := New()
	require.NoError(t, r.Add(&RepoEntry{ID: "a", Owner: "octo", Name: "widget"}))
	// Same coordinates, different ID, different casing — still a collision.
	err := r.Add(&RepoEntry{ID: "b", Owner: "Octo", Name: "Widget"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate repo")
}

func TestRegistry_GetByOwnerRepo(t *testing.T) {
	t.Parallel()
	r := New()
	require.NoError(t, r.Add(&RepoEntry{ID: "managed", Owner: "Octo", Name: "Widget"}))
	require.NoError(t, r.Add(&RepoEntry{ID: "no-remote"})) // empty coordinates: ID-only

	t.Run("matches owner/repo case-insensitively", func(t *testing.T) {
		t.Parallel()
		e, ok := r.GetByOwnerRepo("octo", "widget")
		require.True(t, ok)
		require.Equal(t, "managed", e.ID)
	})

	t.Run("returns false when no repo matches", func(t *testing.T) {
		t.Parallel()
		_, ok := r.GetByOwnerRepo("octo", "missing")
		require.False(t, ok)
	})

	t.Run("returns false for empty coordinates", func(t *testing.T) {
		t.Parallel()
		_, ok := r.GetByOwnerRepo("", "")
		require.False(t, ok)
	})

	t.Run("entry without coordinates is not indexed by owner/repo", func(t *testing.T) {
		t.Parallel()
		_, byID := r.Get("no-remote")
		require.True(t, byID, "should still be reachable by ID")
		_, byOwnerRepo := r.GetByOwnerRepo("", "")
		require.False(t, byOwnerRepo)
	})
}

func TestRegistry_GetMissing(t *testing.T) {
	t.Parallel()
	r := New()
	_, ok := r.Get("nope")
	require.False(t, ok)
}

func TestRegistry_ListReturnsSortedByID(t *testing.T) {
	t.Parallel()
	r := New()
	for _, id := range []string{"c", "a", "b"} {
		require.NoError(t, r.Add(&RepoEntry{ID: id}))
	}

	got := r.List()
	require.Len(t, got, 3)
	require.Equal(t, "a", got[0].ID)
	require.Equal(t, "b", got[1].ID)
	require.Equal(t, "c", got[2].ID)
}

func TestRegistry_FindManaged(t *testing.T) {
	t.Parallel()
	r := New()
	require.NoError(t, r.Add(&RepoEntry{ID: "managed", Managed: true, Owner: "Octo", Name: "Widget"}))
	require.NoError(t, r.Add(&RepoEntry{ID: "unmanaged", Owner: "octo", Name: "dev"}))

	t.Run("matches owner/name case-insensitively", func(t *testing.T) {
		t.Parallel()
		e, ok := r.FindManaged("octo", "widget")
		require.True(t, ok)
		require.Equal(t, "managed", e.ID)
	})

	t.Run("skips unmanaged entries", func(t *testing.T) {
		t.Parallel()
		_, ok := r.FindManaged("octo", "dev")
		require.False(t, ok, "the unmanaged -cwd repo must never be selected for mirror-fetch")
	})

	t.Run("returns false when no repo matches", func(t *testing.T) {
		t.Parallel()
		_, ok := r.FindManaged("octo", "missing")
		require.False(t, ok)
	})
}

func TestRegistry_Len(t *testing.T) {
	t.Parallel()
	r := New()
	require.Equal(t, 0, r.Len())
	require.NoError(t, r.Add(&RepoEntry{ID: "a"}))
	require.NoError(t, r.Add(&RepoEntry{ID: "b"}))
	require.Equal(t, 2, r.Len())
}

func TestRegistry_CloseRunsClosersInReverseAndJoinsErrors(t *testing.T) {
	t.Parallel()

	var order []string
	closerOK := func(name string) func() error {
		return func() error {
			order = append(order, name)
			return nil
		}
	}

	e := &RepoEntry{ID: "x"}
	e.AddCloser(closerOK("first"))
	e.AddCloser(closerOK("second"))
	e.AddCloser(func() error { order = append(order, "third"); return errors.New("boom-third") })
	e.AddCloser(func() error { order = append(order, "fourth"); return errors.New("boom-fourth") })

	r := New()
	require.NoError(t, r.Add(e))

	err := r.Close()
	require.Error(t, err)
	// reverse-registration order
	require.Equal(t, []string{"fourth", "third", "second", "first"}, order)
	// both errors surfaced
	require.Contains(t, err.Error(), "boom-fourth")
	require.Contains(t, err.Error(), "boom-third")
}

func TestRegistry_CloseEmptiesRegistry(t *testing.T) {
	t.Parallel()
	r := New()
	require.NoError(t, r.Add(&RepoEntry{ID: "a"}))
	require.NoError(t, r.Close())
	require.Equal(t, 0, r.Len())
	_, ok := r.Get("a")
	require.False(t, ok)
}

func TestRegistry_AddCloserIgnoresNil(t *testing.T) {
	t.Parallel()
	e := &RepoEntry{ID: "a"}
	e.AddCloser(nil)

	r := New()
	require.NoError(t, r.Add(e))
	require.NoError(t, r.Close())
}

func TestRegistry_ConcurrentAddAndGet(t *testing.T) {
	t.Parallel()
	r := New()

	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = r.Add(&RepoEntry{ID: fmt.Sprintf("r%d", i)})
		}(i)
		go func(i int) {
			defer wg.Done()
			_, _ = r.Get(fmt.Sprintf("r%d", i))
		}(i)
	}
	wg.Wait()

	require.Equal(t, n, r.Len())
}
