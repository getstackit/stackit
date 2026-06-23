package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getstackit/stackit/internal/api/auth"
	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/stretchr/testify/require"
)

func TestVisibleTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		addedBy string
		user    string
		want    bool
	}{
		{"anonymous sees shared repo", "", "", true},
		{"anonymous cannot see user repo", "alice", "", false},
		{"shared repo visible to all", "", "bob", true},
		{"owner sees own repo", "alice", "alice", true},
		{"other user cannot see", "alice", "bob", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, visibleTo(&registry.RepoEntry{AddedBy: tt.addedBy}, tt.user))
		})
	}
}

// sessionFor wraps next in RequireSession and returns the wrapped handler plus
// a request already bearing a valid session cookie for login.
func sessionFor(t *testing.T, login string, next http.Handler) (http.Handler, *http.Request) {
	t.Helper()
	cipher, err := auth.NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	require.NoError(t, err)
	store := auth.NewMemoryStore(cipher)
	t.Cleanup(func() { _ = store.Close() })
	sess, err := store.Create(login, 1, "tok")
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	req.AddCookie(&http.Cookie{Name: "stackit_session", Value: sess.ID})
	return auth.RequireSession(store, next), req
}

func listIDs(t *testing.T, h http.Handler, req *http.Request) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp httpcontract.ReposListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	ids := make([]string, 0, len(resp.Repos))
	for _, r := range resp.Repos {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestReposListFiltersByUser(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "shared"}))
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "alice-repo", AddedBy: "alice"}))
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "bob-repo", AddedBy: "bob"}))
	handler := NewReposListHandler(reg)

	t.Run("anonymous sees shared only", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
		require.ElementsMatch(t, []string{"shared"}, listIDs(t, handler, req))
	})

	t.Run("alice sees shared and her own", func(t *testing.T) {
		t.Parallel()
		gated, req := sessionFor(t, "alice", handler)
		require.ElementsMatch(t, []string{"shared", "alice-repo"}, listIDs(t, gated, req))
	})

	t.Run("bob sees shared and his own", func(t *testing.T) {
		t.Parallel()
		gated, req := sessionFor(t, "bob", handler)
		require.ElementsMatch(t, []string{"shared", "bob-repo"}, listIDs(t, gated, req))
	})
}

// TestResolveRepoVisibility checks the scoped-route gate: a repo a user did not
// add is invisible (404), while shared and own repos resolve.
func TestResolveRepoVisibility(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "alice-repo", AddedBy: "alice", Owner: "alice", Name: "repo"}))

	// A tiny handler that 200s when resolveRepo succeeds.
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := resolveRepo(reg, w, r); !ok {
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	t.Run("owner resolves", func(t *testing.T) {
		t.Parallel()
		gated, base := sessionFor(t, "alice", probe)
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, withRepoCoords(base, "alice", "repo"))
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("other user gets 404", func(t *testing.T) {
		t.Parallel()
		gated, base := sessionFor(t, "bob", probe)
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, withRepoCoords(base, "alice", "repo"))
		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("anonymous gets 404", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		probe.ServeHTTP(rec, withRepoCoords(httptest.NewRequest(http.MethodGet, "/api/v1/repos/alice/repo/view", nil), "alice", "repo"))
		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}
