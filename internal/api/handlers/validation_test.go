package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/registry"
)

// validatingHandlers is the set of API handlers that must reject malformed
// branch names at the HTTP boundary, before any value reaches the git layer.
// Each case wires up the handler exactly the way the router wires it in
// production, with branchValue arriving via either a path value or a query
// param depending on the handler.
type validationCase struct {
	name   string
	build  func(reg *registry.Registry) http.Handler
	method string
	url    string
	// setRequest mutates the request to attach the bad branch value the
	// handler reads (path value or query param).
	setRequest func(req *http.Request, branch string)
}

var validationCases = []validationCase{
	{
		name:   "BranchesHandler getBranch path value",
		build:  func(reg *registry.Registry) http.Handler { return NewBranchesHandler(reg) },
		method: http.MethodGet,
		url:    "/api/v1/branches/x",
		setRequest: func(req *http.Request, branch string) {
			req.SetPathValue("name", branch)
		},
	},
	{
		name:   "BranchDiffHandler branch query",
		build:  func(reg *registry.Registry) http.Handler { return NewBranchDiffHandler(reg) },
		method: http.MethodGet,
		url:    "/api/v1/branch-diff?branch=x",
		setRequest: func(req *http.Request, branch string) {
			q := req.URL.Query()
			q.Set("branch", branch)
			req.URL.RawQuery = q.Encode()
		},
	},
	{
		name:   "SubmitHandler rootBranch path value",
		build:  func(reg *registry.Registry) http.Handler { return NewSubmitHandler(reg) },
		method: http.MethodPost,
		url:    "/api/v1/stacks/x/submit",
		setRequest: func(req *http.Request, branch string) {
			req.SetPathValue("rootBranch", branch)
		},
	},
}

func TestHandlers_RejectMalformedBranchNames(t *testing.T) {
	t.Parallel()

	// Names ValidateBranchName must reject. Cover the most security-relevant
	// failure modes: leading hyphen (argname injection), traversal (..),
	// invalid characters, and length.
	badNames := []struct {
		name  string
		input string
	}{
		{"leading hyphen", "--upload-pack=evil"},
		{"consecutive dots", "feature/..main"},
		{"invalid character semicolon", "feature;rm -rf /"},
		{"invalid character newline", "feature\nrm"},
		{"trailing slash", "feature/"},
		{"leading slash", "/feature"},
		{"too long", strings.Repeat("a", 300)},
	}

	for _, vc := range validationCases {
		for _, bad := range badNames {
			t.Run(vc.name+"/"+bad.name, func(t *testing.T) {
				t.Parallel()

				reg := registry.New()
				require.NoError(t, reg.Add(&registry.RepoEntry{ID: "default"}))

				req := httptest.NewRequest(vc.method, vc.url, nil)
				vc.setRequest(req, bad.input)

				rr := httptest.NewRecorder()
				vc.build(reg).ServeHTTP(rr, req)

				require.Equalf(t, http.StatusBadRequest, rr.Code,
					"%s: expected 400 for %q, got %d (body=%q)",
					vc.name, bad.input, rr.Code, rr.Body.String())
				require.NotContainsf(t, rr.Body.String(), bad.input,
					"response body must not reflect the supplied branch name back at the caller")
			})
		}
	}
}
