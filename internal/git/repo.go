package git

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
)

func (r *runner) getRef(repo *Repository, name string) (string, error) {
	return r.getRevision(repo, name)
}

func (r *runner) readBlob(repo *Repository, sha string) (string, error) {
	// Synchronize go-git operations to prevent concurrent packfile access
	goGitMu.Lock()
	defer goGitMu.Unlock()

	hash := plumbing.NewHash(sha)
	blob, err := repo.BlobObject(hash)
	if err != nil {
		return "", fmt.Errorf("failed to get blob %s: %w", sha, err)
	}

	reader, err := blob.Reader()
	if err != nil {
		return "", fmt.Errorf("failed to get blob reader: %w", err)
	}
	defer func() { _ = reader.Close() }()

	content, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read blob: %w", err)
	}

	return string(content), nil
}

func (r *runner) listRefs(repo *Repository, prefix string) (map[string]string, error) {
	// Synchronize go-git operations to prevent concurrent packfile access
	goGitMu.Lock()
	defer goGitMu.Unlock()

	result := make(map[string]string)
	refs, err := repo.References()
	if err != nil {
		return nil, fmt.Errorf("failed to list references: %w", err)
	}
	defer refs.Close()

	err = refs.ForEach(func(ref *plumbing.Reference) error {
		refName := string(ref.Name())
		if strings.HasPrefix(refName, prefix) {
			result[refName] = ref.Hash().String()
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to iterate references: %w", err)
	}

	return result, nil
}

func (r *runner) GetRepoInfo(ctx context.Context) (string, string, error) {
	// Get remote URL
	url, _ := r.runGitCommandWithContextInternal(ctx, "config", "--get", "remote.origin.url")
	// url will be empty if there's an error (e.g. remote.origin.url not set)
	// This happens in many tests and is not a fatal error for most operations.
	if url == "" {
		return "", "", nil
	}

	// Parse URL (handles both https and ssh formats)
	url = strings.TrimSpace(url)
	if url == "" {
		return "", "", nil
	}
	url = strings.TrimSuffix(url, ".git")
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid remote URL")
	}

	repoName := parts[len(parts)-1]
	var owner string
	if strings.Contains(url, "@") {
		// SSH format: git@github.com:owner/repo
		sshParts := strings.Split(url, ":")
		if len(sshParts) < 2 {
			return "", "", fmt.Errorf("invalid SSH remote URL")
		}
		pathParts := strings.Split(sshParts[1], "/")
		if len(pathParts) < 2 {
			return "", "", fmt.Errorf("invalid SSH remote URL")
		}
		owner = pathParts[0]
	} else {
		// HTTPS format: https://github.com/owner/repo
		owner = parts[len(parts)-2]
	}

	return owner, repoName, nil
}
