package github

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v67/github"
)

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
	apps, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("github app transport: %w", err)
	}
	return &AppTokenProvider{
		apps:           apps,
		resolveClient:  github.NewClient(&http.Client{Transport: apps}),
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
		inst, _, err := p.resolveClient.Apps.FindRepositoryInstallation(ctx, owner, name)
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
