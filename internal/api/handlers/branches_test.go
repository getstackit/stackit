package handlers

import (
	"context"
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

	req := withRepo(httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/demo/branches/diff", nil))
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

	req := withRepo(httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/demo/branches/"+url.PathEscape(branchName), nil))
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
	handler := NewBranchDiffHandler(reg, 0)

	req := withRepo(httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/demo/branch-diff/"+url.PathEscape(branchName), nil))
	req.SetPathValue("name", branchName)
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

func TestBranchDiffHandler_ThrottleGateRespectsContext(t *testing.T) {
	t.Parallel()

	branchName := "feature"
	s := setupTrackedBranchScenario(t, branchName)
	reg := singleEntryRegistry(t, s)
	handler := NewBranchDiffHandler(reg, 1)

	// Saturate the single concurrency slot so the next request must wait for
	// it to free.
	handler.sem <- struct{}{}

	// A canceled context stands in for a client that has gone away while
	// queued behind the throttle.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := withRepo(httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/demo/branch-diff/"+url.PathEscape(branchName), nil).WithContext(ctx))
	req.SetPathValue("name", branchName)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// The handler returns from the throttle gate without computing a diff,
	// rather than spawning git anyway.
	require.Empty(t, rr.Body.String(), "no diff should be computed when throttled and the client is gone")
}

func TestBranchDiffHandler_RequiresBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	reg := singleEntryRegistry(t, s)
	handler := NewBranchDiffHandler(reg, 0)

	// No {name} path value: the resource resolves to the repo but carries no
	// branch, so the handler rejects it before touching git.
	req := withRepo(httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/demo/branch-diff/", nil))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "missing branch")
}

func TestBranchDiffHandler_RejectsUntrackedBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	reg := singleEntryRegistry(t, s)
	handler := NewBranchDiffHandler(reg, 0)

	req := withRepo(httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/demo/branch-diff/main", nil))
	req.SetPathValue("name", "main")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "branch not found or not tracked")
}

func TestResolveRepo_Returns404ForUnknownRepo(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "default", Owner: "acme", Name: "demo"}))
	handler := NewBranchesHandler(reg)

	req := withRepoCoords(httptest.NewRequest(http.MethodGet, "/api/v1/repos/acme/missing/branches", nil), "acme", "missing")
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

// testOwner and testRepo are the GitHub coordinates singleEntryRegistry
// registers its entry under. Direct handler calls (no router) resolve to it
// once withRepo sets the matching owner/repo path values resolveRepo reads.
const (
	testOwner = "acme"
	testRepo  = "demo"
)

// singleEntryRegistry builds a registry with one entry pointing at the
// scenario's engine, indexed by testOwner/testRepo. Mirrors the bootstrap
// behavior of the single-repo `-cwd` shortcut.
func singleEntryRegistry(t *testing.T, s *scenario.Scenario) *registry.Registry {
	t.Helper()
	reg := registry.New()
	require.NoError(t, reg.Add(&registry.RepoEntry{
		ID:     "default",
		Owner:  testOwner,
		Name:   testRepo,
		Engine: s.Engine,
	}))
	return reg
}

// withRepoCoords sets the owner/repo path values resolveRepo reads, standing in
// for the router's {owner}/{repo} match in a direct handler call.
func withRepoCoords(req *http.Request, owner, repo string) *http.Request {
	req.SetPathValue("owner", owner)
	req.SetPathValue("repo", repo)
	return req
}

// withRepo sets the path values for the singleEntryRegistry entry.
func withRepo(req *http.Request) *http.Request {
	return withRepoCoords(req, testOwner, testRepo)
}
