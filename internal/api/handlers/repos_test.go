package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestReposListHandler_ListsRegisteredRepos(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{
		ID:          "alpha",
		DisplayName: "Alpha",
		Engine:      s.Engine,
	}))
	require.NoError(t, reg.Add(&registry.RepoEntry{
		ID:          "beta",
		DisplayName: "Beta",
		Engine:      s.Engine,
	}))

	handler := NewReposListHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp httpcontract.ReposListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.Len(t, resp.Repos, 2)
	require.Equal(t, "alpha", resp.Repos[0].ID)
	require.Equal(t, "Alpha", resp.Repos[0].DisplayName)
	require.Equal(t, "main", resp.Repos[0].Trunk)
	require.Equal(t, "beta", resp.Repos[1].ID)
}

func TestReposListHandler_EmptyRegistry(t *testing.T) {
	t.Parallel()

	handler := NewReposListHandler(registry.New())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp httpcontract.ReposListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.Empty(t, resp.Repos)
}

func TestReposListHandler_RejectsNonGET(t *testing.T) {
	t.Parallel()

	handler := NewReposListHandler(registry.New())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}
