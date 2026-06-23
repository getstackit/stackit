package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getstackit/stackit/internal/api/auth"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/api/store"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/stretchr/testify/require"
)

type fakeRepoStore struct {
	added []store.Repo
	err   error
}

func (f *fakeRepoStore) AddRepo(_ context.Context, r store.Repo) (store.Repo, error) {
	if f.err != nil {
		return store.Repo{}, f.err
	}
	f.added = append(f.added, r)
	return r, nil
}

type fakeTokenProvider struct {
	token string
	err   error
}

func (f fakeTokenProvider) InstallationToken(_ context.Context, _, _ string) (string, error) {
	return f.token, f.err
}

func onboardCipher(t *testing.T) *auth.Cipher {
	t.Helper()
	c, err := auth.NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	require.NoError(t, err)
	return c
}

// doOnboard sends body as user "alice" through RequireSession into h, using a
// session store keyed by the same cipher h holds so the token round-trips.
func doOnboard(t *testing.T, h *OnboardHandler, cipher *auth.Cipher, body string) *httptest.ResponseRecorder {
	t.Helper()
	sessions := auth.NewMemoryStore(cipher)
	t.Cleanup(func() { _ = sessions.Close() })
	sess, err := sessions.Create("alice", 1, "user-token")
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "stackit_session", Value: sess.ID})
	rec := httptest.NewRecorder()
	auth.RequireSession(sessions, h).ServeHTTP(rec, req)
	return rec
}

func TestOnboardHandlerSuccess(t *testing.T) {
	t.Parallel()
	src := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	blob, err := src.Repo.RunGitCommandAndGetOutput("hash-object", "-w", "--stdin")
	require.NoError(t, err)
	require.NoError(t, src.Repo.RunGitCommand("update-ref", "refs/stackit/stacks/test-stack", blob))

	reg := registry.New()
	t.Cleanup(func() { _ = reg.Close() })
	repoStore := &fakeRepoStore{}
	cipher := onboardCipher(t)
	reposRoot := t.TempDir()

	h := NewOnboardHandler(reg, repoStore, cipher, reposRoot, fakeTokenProvider{token: "inst-token"})
	h.checkAccess = func(_ context.Context, token, owner, name string) (*github.RepoAccess, error) {
		require.Equal(t, "user-token", token)
		return &github.RepoAccess{Owner: owner, Name: name, CloneURL: src.Dir, DefaultBranch: "main"}, nil
	}

	rec := doOnboard(t, h, cipher, `{"owner":"octo","name":"widget"}`)
	require.Equal(t, http.StatusCreated, rec.Code)

	entry, ok := reg.Get("octo-widget")
	require.True(t, ok)
	require.Equal(t, "alice", entry.AddedBy)
	require.Equal(t, "octo/widget", entry.DisplayName)
	require.True(t, entry.Managed, "onboarded repos are server-managed mirrors")

	require.Len(t, repoStore.added, 1)
	require.Equal(t, "octo-widget", repoStore.added[0].ID)
	require.Equal(t, "alice", repoStore.added[0].AddedBy)
	require.Equal(t, "octo", repoStore.added[0].Owner)
	require.Equal(t, "widget", repoStore.added[0].Name)
	require.Empty(t, repoStore.added[0].Path, "path is stored empty so it re-derives under the host's repos root")

	require.DirExists(t, filepath.Join(reposRoot, "octo", "widget", ".git"))
	err = exec.Command("git", "-C", filepath.Join(reposRoot, "octo", "widget"), "rev-parse", "--verify", "--quiet", "refs/stackit/stacks/test-stack").Run()
	require.NoError(t, err, "onboarding should mirror stack metadata before serving the repo")
}

func TestOnboardHandlerAccessDenied(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	t.Cleanup(func() { _ = reg.Close() })
	cipher := onboardCipher(t)

	h := NewOnboardHandler(reg, &fakeRepoStore{}, cipher, t.TempDir(), fakeTokenProvider{token: "inst-token"})
	h.checkAccess = func(_ context.Context, _, _, _ string) (*github.RepoAccess, error) {
		return nil, github.ErrRepoAccessDenied
	}

	rec := doOnboard(t, h, cipher, `{"owner":"octo","name":"secret"}`)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Empty(t, reg.List())
}

func TestOnboardHandlerDuplicate(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	t.Cleanup(func() { _ = reg.Close() })
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "octo-widget", AddedBy: "alice"}))
	cipher := onboardCipher(t)

	h := NewOnboardHandler(reg, &fakeRepoStore{}, cipher, t.TempDir(), fakeTokenProvider{token: "inst-token"})
	rec := doOnboard(t, h, cipher, `{"owner":"octo","name":"widget"}`)
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestOnboardHandlerDuplicateDifferentCasing(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	t.Cleanup(func() { _ = reg.Close() })
	// An existing repo, indexed by its case-folded owner/repo key.
	require.NoError(t, reg.Add(&registry.RepoEntry{ID: "octo-widget", Owner: "octo", Name: "widget", AddedBy: "alice"}))
	cipher := onboardCipher(t)

	h := NewOnboardHandler(reg, &fakeRepoStore{}, cipher, t.TempDir(), fakeTokenProvider{token: "inst-token"})
	h.checkAccess = func(_ context.Context, _, _, _ string) (*github.RepoAccess, error) {
		t.Fatal("onboard should 409 on the owner/repo collision before checking access or cloning")
		return nil, nil
	}

	// Different casing derives a different repo ID, so the ID check passes and
	// only the owner/repo index catches the collision. Without that guard this
	// would clone and persist a row, then 500 at reg.Add with an orphaned row.
	rec := doOnboard(t, h, cipher, `{"owner":"Octo","name":"Widget"}`)
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestOnboardHandlerInvalidCoords(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	t.Cleanup(func() { _ = reg.Close() })
	cipher := onboardCipher(t)

	h := NewOnboardHandler(reg, &fakeRepoStore{}, cipher, t.TempDir(), fakeTokenProvider{token: "inst-token"})
	rec := doOnboard(t, h, cipher, `{"owner":"../etc","name":"passwd"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOnboardHandlerRequiresSession(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	t.Cleanup(func() { _ = reg.Close() })
	h := NewOnboardHandler(reg, &fakeRepoStore{}, onboardCipher(t), t.TempDir(), fakeTokenProvider{token: "inst-token"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(`{"owner":"octo","name":"widget"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestOnboardHandlerRequiresStore(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	t.Cleanup(func() { _ = reg.Close() })
	h := NewOnboardHandler(reg, nil, onboardCipher(t), t.TempDir(), fakeTokenProvider{token: "inst-token"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(`{"owner":"octo","name":"widget"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestOnboardHandlerRequiresGitHubApp(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	t.Cleanup(func() { _ = reg.Close() })
	// nil token provider => no GitHub App configured.
	h := NewOnboardHandler(reg, &fakeRepoStore{}, onboardCipher(t), t.TempDir(), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(`{"owner":"octo","name":"widget"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestOnboardHandlerAppNotInstalled(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	t.Cleanup(func() { _ = reg.Close() })
	cipher := onboardCipher(t)

	// User can see the repo, but the App isn't installed on the owner.
	h := NewOnboardHandler(reg, &fakeRepoStore{}, cipher, t.TempDir(), fakeTokenProvider{err: errors.New("installation not found")})
	h.checkAccess = func(_ context.Context, _, owner, name string) (*github.RepoAccess, error) {
		return &github.RepoAccess{Owner: owner, Name: name}, nil
	}

	rec := doOnboard(t, h, cipher, `{"owner":"octo","name":"widget"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "GitHub App")
	require.Empty(t, reg.List())
}
