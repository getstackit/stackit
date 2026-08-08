package git

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// RemoteRepository identifies the remote host and repository used for GitHub
// operations. The zero value represents a remote that cannot be mapped to a
// hosted owner/name repository, such as a local test remote.
type RemoteRepository struct {
	Host  string
	Owner string
	Name  string
}

// IsHosted reports whether the remote identifies a hosted owner/name
// repository.
func (r RemoteRepository) IsHosted() bool {
	return r.Host != "" && r.Owner != "" && r.Name != ""
}

// GetRepoInfo reads and parses the default remote repository identity.
func (r *runner) GetRepoInfo(ctx context.Context) (RemoteRepository, error) {
	out, err := r.RunGitCommandWithContext(ctx, "remote", "get-url", DefaultRemote)
	if err != nil {
		return RemoteRepository{}, nil //nolint:nilerr // missing remote is non-fatal here
	}
	return ParseRemoteRepository(out)
}

// ParseRemoteRepository extracts host, owner, and repository name from common
// Git remote URL forms. Local paths are not an error; they return the zero
// value because callers may use them in tests or non-GitHub workflows.
func ParseRemoteRepository(remoteURL string) (RemoteRepository, error) {
	remoteURL = strings.TrimSuffix(strings.TrimSpace(remoteURL), ".git")
	if remoteURL == "" {
		return RemoteRepository{}, nil
	}

	// Try standard URL parsing first (handles ssh://, https://, git://, etc.)
	parsed, err := url.Parse(remoteURL)
	if err == nil && parsed.Scheme != "" {
		if parsed.Hostname() == "" {
			return RemoteRepository{}, nil
		}
		parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
		if len(parts) < 2 {
			return RemoteRepository{}, fmt.Errorf("invalid remote URL: path must contain owner/repo")
		}
		return RemoteRepository{
			Host:  parsed.Hostname(),
			Owner: parts[len(parts)-2],
			Name:  parts[len(parts)-1],
		}, nil
	}

	// Fall back to SCP-like formats: git@github.com:owner/repo and
	// git@github.com/owner/repo.
	if strings.Contains(remoteURL, "@") {
		_, hostAndPath, found := strings.Cut(remoteURL, "@")
		if !found {
			return RemoteRepository{}, fmt.Errorf("invalid SSH remote URL")
		}
		host, path, found := strings.Cut(hostAndPath, ":")
		if !found {
			host, path, found = strings.Cut(hostAndPath, "/")
		}
		if !found || host == "" {
			return RemoteRepository{}, fmt.Errorf("invalid SSH remote URL: missing path")
		}
		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			return RemoteRepository{}, fmt.Errorf("invalid SSH remote URL: path must contain owner/repo")
		}
		return RemoteRepository{Host: host, Owner: parts[len(parts)-2], Name: parts[len(parts)-1]}, nil
	}

	// Local file paths (used in tests) or other formats - return empty
	// This is not an error; the caller may use other means to get repo info
	return RemoteRepository{}, nil
}
