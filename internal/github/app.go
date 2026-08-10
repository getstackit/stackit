package github

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v90/github"
)

// appHTTPTimeout is the client-level ceiling for GitHub App HTTP calls
// (installation discovery and JWT token minting). Callers pass a context
// deadline, but http.DefaultTransport sets no timeout of its own, so a stalled
// socket reached with a context.Background() that slips through could hang
// indefinitely. This is the belt-and-suspenders bound.
const appHTTPTimeout = 30 * time.Second

// appHTTPTransport clones http.DefaultTransport and adds a response-header
// timeout so a remote that accepts the connection but never replies cannot
// wedge a request. DefaultTransport is shared process-wide and must not be
// mutated in place, hence the clone.
func appHTTPTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = appHTTPTimeout
	return tr
}

// AppTokenProvider mints GitHub App installation access tokens for repositories.
// Installation tokens are durable — the underlying transport refreshes them
// before expiry — so background jobs (the repo sync loop) can authenticate git
// fetches with no user present. The App must be installed on the repo's owner.
type AppTokenProvider struct {
	apps *ghinstallation.AppsTransport

	// resolveClient is authenticated as the App (JWT) and used only to map an
	// owner/repo to its installation ID.
	resolveClient *github.Client

	mu             sync.Mutex
	installByOwner map[string]int64
	transports     map[int64]*ghinstallation.Transport
}

// NewAppTokenProvider builds a provider from a GitHub App ID and its
// PEM-encoded private key (PKCS#1 or PKCS#8).
func NewAppTokenProvider(appID int64, privateKeyPEM []byte) (*AppTokenProvider, error) {
	apps, err := ghinstallation.NewAppsTransport(appHTTPTransport(), appID, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("github app transport: %w", err)
	}
	resolveClient, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: apps, Timeout: appHTTPTimeout}))
	if err != nil {
		return nil, fmt.Errorf("github app client: %w", err)
	}
	return &AppTokenProvider{
		apps:           apps,
		resolveClient:  resolveClient,
		installByOwner: make(map[string]int64),
		transports:     make(map[int64]*ghinstallation.Transport),
	}, nil
}

// InstallationToken returns a fresh installation access token scoped to the
// owner of owner/name. The installation ID is resolved once per owner and
// cached; the token itself is minted and refreshed by the underlying transport.
func (p *AppTokenProvider) InstallationToken(ctx context.Context, owner, name string) (string, error) {
	tr, err := p.transportFor(ctx, owner, name)
	if err != nil {
		return "", err
	}
	token, err := tr.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("mint installation token for %s/%s: %w", owner, name, err)
	}
	return token, nil
}

func (p *AppTokenProvider) transportFor(ctx context.Context, owner, name string) (*ghinstallation.Transport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	id, ok := p.installByOwner[owner]
	if !ok {
		// One network round-trip per owner, then cached. Held under the lock so
		// concurrent first-time resolutions for the same owner don't duplicate
		// the call; token minting (below) happens outside the lock.
		inst, _, err := p.resolveClient.Apps.GetRepositoryInstallation(ctx, owner, name)
		if err != nil {
			return nil, fmt.Errorf("find app installation for %s/%s: %w", owner, name, err)
		}
		id = inst.GetID()
		p.installByOwner[owner] = id
	}

	tr, ok := p.transports[id]
	if !ok {
		tr = ghinstallation.NewFromAppsTransport(p.apps, id)
		p.transports[id] = tr
	}
	return tr, nil
}
