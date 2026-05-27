package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestBranchesHandler_AllowsBranchNamedDiff(t *testing.T) {
	t.Parallel()

	s := setupTrackedBranchScenario(t, "diff")
	reg := singleEntryRegistry(t, s)
	handler := NewBranchesHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branches/diff", nil)
	req.SetPathValue("name", "diff")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp httpcontract.BranchResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.Equal(t, "diff", resp.Name)
}

func TestBranchesHandler_AllowsBranchEndingInDiffSuffix(t *testing.T) {
	t.Parallel()

	branchName := "team/feature/diff"
	s := setupTrackedBranchScenario(t, branchName)
	reg := singleEntryRegistry(t, s)
	handler := NewBranchesHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branches/"+url.PathEscape(branchName), nil)
	req.SetPathValue("name", branchName)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp httpcontract.BranchResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.Equal(t, branchName, resp.Name)
}

func TestBranchDiffHandler_ReturnsDiff(t *testing.T) {
	t.Parallel()

	branchName := "feature"
	s := setupTrackedBranchScenario(t, branchName)
	reg := singleEntryRegistry(t, s)
	handler := NewBranchDiffHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branch-diff?branch="+url.QueryEscape(branchName), nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp httpcontract.BranchDiffResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.Equal(t, branchName, resp.Branch)
	require.NotEmpty(t, resp.BaseRevision)
	require.NotEmpty(t, resp.HeadRevision)
	require.Contains(t, resp.Patch, "diff --git")
}

func TestBranchDiffHandler_RequiresBranchQuery(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	reg := singleEntryRegistry(t, s)
	handler := NewBranchDiffHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branch-diff", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "missing branch query parameter")
}

func TestBranchDiffHandler_RejectsUntrackedBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	reg := singleEntryRegistry(t, s)
	handler := NewBranchDiffHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branch-diff?branch=main", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "branch not found or not tracked")
}

func TestResolveRepo_Returns404ForUnknownID(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "default"}))
	handler := NewBranchesHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/missing/branches", nil)
	req.SetPathValue("repoID", "missing")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
}

func setupTrackedBranchScenario(t *testing.T, branchName string) *scenario.Scenario {
	t.Helper()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	filePrefix := "change-" + strings.ReplaceAll(branchName, "/", "-")

	s.CreateBranch(branchName).
		CommitChange(filePrefix, "change on "+branchName).
		Checkout("main").
		TrackBranch(branchName, "main").
		Rebuild()

	return s
}

// singleEntryRegistry builds a registry with one entry id="default" pointing
// at the scenario's engine. Mirrors the bootstrap behavior of the single-repo
// `-cwd` shortcut and lets tests skip path-value setup for repoID.
func singleEntryRegistry(t *testing.T, s *scenario.Scenario) *registry.Registry {
	t.Helper()
	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{
		ID:     "default",
		Engine: s.Engine,
	}))
	return reg
}
