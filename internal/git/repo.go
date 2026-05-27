package git

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

func (r *runner) GetRepoInfo(ctx context.Context) (string, string, error) {
	out, err := r.RunGitCommandWithContext(ctx, "remote", "get-url", DefaultRemote)
	if err != nil {
		return "", "", nil //nolint:nilerr // missing remote is non-fatal here
	}
	remoteURL := strings.TrimSpace(out)
	if remoteURL == "" {
		return "", "", nil
	}
	remoteURL = strings.TrimSuffix(remoteURL, ".git")

	// Try standard URL parsing first (handles ssh://, https://, git://, etc.)
	parsed, err := url.Parse(remoteURL)
	if err == nil && parsed.Scheme != "" {
		parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
		if len(parts) < 2 {
			return "", "", fmt.Errorf("invalid remote URL: path must contain owner/repo")
		}
		return parts[0], parts[len(parts)-1], nil
	}

	// Fall back to SCP-like format: git@github.com:owner/repo
	if strings.Contains(remoteURL, "@") {
		if _, path, found := strings.Cut(remoteURL, ":"); found {
			parts := strings.Split(path, "/")
			if len(parts) < 2 {
				return "", "", fmt.Errorf("invalid SCP-style remote URL: path must contain owner/repo")
			}
			return parts[0], parts[len(parts)-1], nil
		}
	}

	// Local file paths (used in tests) or other formats - return empty
	// This is not an error; the caller may use other means to get repo info
	return "", "", nil
}
