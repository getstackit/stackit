package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestBranchesHandler_AllowsBranchNamedDiff(t *testing.T) {
	t.Parallel()

	s := setupTrackedBranchScenario(t, "diff")
	handler := NewBranchesHandler(s.Engine, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branches/diff", nil)
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
	handler := NewBranchesHandler(s.Engine, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branches/"+url.PathEscape(branchName), nil)
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
	handler := NewBranchDiffHandler(s.Engine)

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
	handler := NewBranchDiffHandler(s.Engine)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branch-diff", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "missing branch query parameter")
}

func TestBranchDiffHandler_RejectsUntrackedBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	handler := NewBranchDiffHandler(s.Engine)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branch-diff?branch=main", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "branch not found or not tracked")
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
