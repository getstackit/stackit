package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getstackit/stackit/internal/api/auth"
	"github.com/getstackit/stackit/internal/api/provision"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/api/reqid"
	"github.com/getstackit/stackit/internal/api/store"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/internal/github"
)

// RepoPersister is the slice of the repo store the onboarding handler needs.
// A nil RepoPersister means persistence is unavailable (no -database-url) and
// onboarding is refused.
type RepoPersister interface {
	AddRepo(ctx context.Context, r store.Repo) (store.Repo, error)
}

// InstallationTokenProvider mints a durable GitHub App installation token for a
// repo. Onboarding clones with it (not the user's token) so the same credential
// works for the background sync loop later. The concrete implementation is
// *github.AppTokenProvider.
type InstallationTokenProvider interface {
	InstallationToken(ctx context.Context, owner, name string) (string, error)
}

// OnboardHandler serves POST /api/v1/repos: a logged-in user asks the server to
// clone a GitHub repo and start serving it. The user's token authorizes the
// request (they must be able to see the repo); the clone itself uses a durable
// GitHub App installation token so the same credential serves later background
// syncs. The repo is recorded against the user's login so only they see it
// (see visibleTo).
type OnboardHandler struct {
	reg       *registry.Registry
	store     RepoPersister
	cipher    *auth.Cipher
	reposRoot string
	tokens    InstallationTokenProvider

	// checkAccess is the one network dependency; it is an injectable seam so
	// tests can authorize without reaching GitHub. Clone/init/build are local
	// git operations and run for real.
	checkAccess func(ctx context.Context, token, owner, name string) (*github.RepoAccess, error)
}

// NewOnboardHandler wires an onboarding handler. store, cipher, and tokens may
// be nil (persistence / auth / GitHub App not configured), in which case
// requests are refused with a clear 503; the handler guards at request time so
// it is safe to mount unconditionally.
func NewOnboardHandler(reg *registry.Registry, st RepoPersister, cipher *auth.Cipher, reposRoot string, tokens InstallationTokenProvider) *OnboardHandler {
	return &OnboardHandler{
		reg:         reg,
		store:       st,
		cipher:      cipher,
		reposRoot:   reposRoot,
		tokens:      tokens,
		checkAccess: github.CheckRepoAccess,
	}
}

var (
	// ownerPattern mirrors GitHub login rules: alphanumerics and single
	// hyphens, no leading/trailing hyphen. No dots — that also blocks path
	// traversal through the owner segment.
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)
	// namePattern allows dots (repo names like "repo.js") but ".." is rejected
	// separately so it can't escape the repos root.
	namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	// idUnsafe collapses anything outside the registry's ID alphabet (which
	// excludes dots) so a dotted repo name still yields a valid repo ID.
	idUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)
)

func (h *OnboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Onboarding acts as the requesting user and persists the result, so it
	// needs auth (a cipher to recover the token), a store, and a place to put
	// checkouts. Each missing prerequisite is a clear, actionable 503.
	switch {
	case h.cipher == nil:
		writeJSONError(w, http.StatusServiceUnavailable, "onboarding requires authentication to be configured")
		return
	case h.store == nil:
		writeJSONError(w, http.StatusServiceUnavailable, "onboarding requires a database (-database-url)")
		return
	case h.tokens == nil:
		writeJSONError(w, http.StatusServiceUnavailable, "onboarding requires a configured GitHub App")
		return
	case h.reposRoot == "":
		writeJSONError(w, http.StatusServiceUnavailable, "onboarding requires a repos root (-repos-root)")
		return
	}

	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req httpcontract.OnboardRepoRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	owner, name, ok := resolveCoords(req)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "owner and name are required (or a github url)")
		return
	}
	if !validRepoCoords(owner, name) {
		writeJSONError(w, http.StatusBadRequest, "invalid owner or repository name")
		return
	}

	id := repoID(owner, name)
	// Guard both identities the registry indexes: the derived ID and the
	// case-folded owner/repo key. repoID does not lowercase, so "Octo/Widget"
	// and "octo/widget" yield distinct IDs but the same owner/repo key. Checking
	// only the ID would let the second casing pass here, clone, persist a row,
	// then fail at reg.Add with a 500 and an orphaned DB row. Catch the
	// owner/repo collision up front so it's a clean 409 before any clone or write.
	if _, exists := h.reg.Get(id); exists {
		writeJSONError(w, http.StatusConflict, "repository already onboarded")
		return
	}
	if _, exists := h.reg.GetByOwnerRepo(owner, name); exists {
		writeJSONError(w, http.StatusConflict, "repository already onboarded")
		return
	}

	token, err := sess.AccessToken(h.cipher)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "session has no github token")
		return
	}

	access, err := h.checkAccess(r.Context(), token, owner, name)
	if err != nil {
		if errors.Is(err, github.ErrRepoAccessDenied) {
			// Mirror "not found" so a caller can't probe for the existence of
			// repos they can't access.
			writeJSONError(w, http.StatusNotFound, "repository not found or not accessible")
			return
		}
		slog.Error("onboard: access check failed", "owner", owner, "name", name, "error", err)
		writeJSONError(w, http.StatusBadGateway, "could not verify repository access")
		return
	}

	dest := filepath.Join(h.reposRoot, owner, name)
	displayName := owner + "/" + name
	cloneURL := access.CloneURL
	if cloneURL == "" {
		cloneURL = "https://github.com/" + owner + "/" + name + ".git"
	}

	// Clone with a durable GitHub App installation token (not the user's
	// session token), so the same credential serves later background syncs. This
	// requires the App to be installed on the owner.
	instToken, err := h.tokens.InstallationToken(r.Context(), owner, name)
	if err != nil {
		slog.Warn("onboard: installation token unavailable", "owner", owner, "name", name, "error", err)
		writeJSONError(w, http.StatusBadRequest, "the Stackit GitHub App is not installed on "+owner+"; install it and try again")
		return
	}

	slog.Info("audit", "action", "onboard", "actor", sess.GitHubLogin, "repo", id, "request_id", reqid.FromContext(r.Context())) //nolint:gosec // structured slog fields

	if err := provision.EnsureClone(r.Context(), provision.CloneParams{CloneURL: cloneURL, Token: instToken, Dest: dest}); err != nil {
		slog.Error("onboard: clone failed", "repo", id, "error", err)
		writeJSONError(w, http.StatusBadGateway, "failed to clone repository")
		return
	}
	if err := provision.MirrorFetch(r.Context(), dest, "origin", instToken); err != nil {
		slog.Error("onboard: mirror fetch failed", "repo", id, "error", err)
		writeJSONError(w, http.StatusBadGateway, "failed to fetch repository metadata")
		return
	}

	// A freshly cloned repo isn't a stackit repo yet; initialize it (set the
	// trunk to the GitHub default branch) so the engine can serve it.
	if err := provision.EnsureInitialized(r.Context(), dest, access.DefaultBranch); err != nil {
		slog.Error("onboard: init failed", "repo", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to initialize repository")
		return
	}

	// Persist before registering so the durable record exists first; a crash
	// between the two recovers on the next restart (the row reloads). Path is
	// stored empty so it re-derives from owner/name under whatever repos root
	// the host uses, matching the store's host-portable design.
	if _, err := h.store.AddRepo(r.Context(), store.Repo{
		ID:          id,
		DisplayName: displayName,
		Owner:       owner,
		Name:        name,
		Remote:      "origin",
		AddedBy:     sess.GitHubLogin,
	}); err != nil {
		slog.Error("onboard: persist failed", "repo", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to record repository")
		return
	}

	entry, err := provision.BuildEntry(r.Context(), provision.EntryParams{
		ID:          id,
		DisplayName: displayName,
		Path:        dest,
		Remote:      "origin",
		AddedBy:     sess.GitHubLogin,
		Managed:     true, // onboarded checkouts are server-owned mirrors.
		Owner:       owner,
		Name:        name,
	})
	if err != nil {
		slog.Error("onboard: engine build failed (persisted; will load on restart)", "repo", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "repository recorded but failed to start serving")
		return
	}
	if err := h.reg.Add(entry); err != nil {
		slog.Error("onboard: registry add failed (persisted; will load on restart)", "repo", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "repository recorded but failed to start serving")
		return
	}

	summary := httpcontract.RepoSummary{ID: entry.ID, Owner: entry.Owner, Repo: entry.Name, DisplayName: entry.DisplayName}
	if entry.Engine != nil {
		summary.Trunk = entry.Engine.Trunk().GetName()
		if cur := entry.Engine.CurrentBranch(); cur != nil {
			summary.CurrentBranch = cur.GetName()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(summary)
}

// resolveCoords extracts owner/name from the request, accepting either explicit
// fields or a github URL / "owner/name" shorthand in the URL field.
func resolveCoords(req httpcontract.OnboardRepoRequest) (owner, name string, ok bool) {
	owner = strings.TrimSpace(req.Owner)
	name = strings.TrimSpace(req.Name)
	if owner != "" && name != "" {
		return owner, name, true
	}
	if u := strings.TrimSpace(req.URL); u != "" {
		return parseRepoURL(u)
	}
	return "", "", false
}

// parseRepoURL accepts "https://github.com/owner/name(.git)", "github.com/owner/name",
// or "owner/name" and returns the two segments.
func parseRepoURL(raw string) (owner, name string, ok bool) {
	s := strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	if i := strings.Index(s, "github.com/"); i >= 0 {
		s = s[i+len("github.com/"):]
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// validRepoCoords rejects coordinates that are malformed or could escape the
// repos root via path traversal.
func validRepoCoords(owner, name string) bool {
	switch {
	case !ownerPattern.MatchString(owner):
		return false
	case !namePattern.MatchString(name):
		return false
	case name == "." || name == ".." || strings.Contains(name, ".."):
		return false
	default:
		return true
	}
}

// repoID derives the registry/store ID from owner/name, collapsing characters
// outside the ID alphabet (e.g. dots) so the result is always a valid ID.
func repoID(owner, name string) string {
	return strings.Trim(idUnsafe.ReplaceAllString(owner+"-"+name, "-"), "-")
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
