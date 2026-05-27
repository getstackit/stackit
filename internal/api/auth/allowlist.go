package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrDenied is returned by Allowlist.Check when the logged-in user is not
// permitted. Handlers translate this into a redirect to /auth/denied.
var ErrDenied = errors.New("user not in allowlist")

// Allowlist is the gate the OAuth callback runs against after exchanging
// the GitHub code for an identity. It supports two independent rules:
//
//   - Users: exact-match GitHub logins (case-insensitive)
//   - Org:   membership in a GitHub organization (verified via API)
//
// At least one rule must be configured; otherwise the gate is open and
// the constructor refuses to build.
type Allowlist struct {
	users map[string]struct{}
	org   string

	// orgChecker is split out so tests can fake the GitHub membership
	// lookup without spinning up an httptest server for every case.
	orgChecker orgMembershipChecker
}

type orgMembershipChecker interface {
	IsMember(ctx context.Context, org, login, accessToken string) (bool, error)
}

// AllowlistConfig is the operator-supplied input. ConfigFromEnv reads
// these out of process env; callers in tests build the struct directly.
type AllowlistConfig struct {
	Users []string
	Org   string
}

// NewAllowlist normalizes the config and validates at least one rule is
// present. An empty allowlist is rejected because a public-facing deploy
// with no gate is exactly the misconfiguration we're trying to prevent.
func NewAllowlist(cfg AllowlistConfig) (*Allowlist, error) {
	users := make(map[string]struct{}, len(cfg.Users))
	for _, u := range cfg.Users {
		u = strings.ToLower(strings.TrimSpace(u))
		if u == "" {
			continue
		}
		users[u] = struct{}{}
	}
	org := strings.TrimSpace(cfg.Org)
	if len(users) == 0 && org == "" {
		return nil, errors.New("allowlist requires at least one user or one org")
	}
	return &Allowlist{
		users:      users,
		org:        org,
		orgChecker: githubOrgChecker{},
	}, nil
}

// SetOrgChecker overrides the org membership checker. Tests use this to
// inject deterministic membership without hitting GitHub.
func (a *Allowlist) SetOrgChecker(c orgMembershipChecker) {
	a.orgChecker = c
}

// Check returns nil if login is allowed, ErrDenied if not. The token is
// only used for the org membership API call; user-list hits short-circuit
// without any network I/O.
func (a *Allowlist) Check(ctx context.Context, login, accessToken string) error {
	loginLC := strings.ToLower(strings.TrimSpace(login))
	if loginLC == "" {
		return ErrDenied
	}
	if _, ok := a.users[loginLC]; ok {
		return nil
	}
	if a.org == "" {
		return ErrDenied
	}
	member, err := a.orgChecker.IsMember(ctx, a.org, login, accessToken)
	if err != nil {
		return fmt.Errorf("check org membership: %w", err)
	}
	if !member {
		return ErrDenied
	}
	return nil
}

// githubOrgChecker hits the real GitHub API. We keep this dependency-free
// (no go-github client) so the package's footprint stays small and tests
// don't need to mock the wider github package.
type githubOrgChecker struct{}

func (githubOrgChecker) IsMember(ctx context.Context, org, login, accessToken string) (bool, error) {
	// GET /orgs/{org}/members/{username} returns 204 if the auth'd user
	// can see that login is a member, 302 if they can but the requester
	// isn't a public member, 404 otherwise. We treat 204 and 302 as
	// "yes", everything else as "no".
	url := fmt.Sprintf("https://api.github.com/orgs/%s/members/%s", org, login)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusFound:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status from membership API: %d", resp.StatusCode)
	}
}
