package git

import (
	"errors"
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// DefaultRemote is the default name for the remote repository
const DefaultRemote = "origin"

func (r *runner) getRemote(repo *Repository) string {
	// Synchronize go-git operations to prevent concurrent packfile access
	goGitMu.Lock()
	defer goGitMu.Unlock()

	// Try to get current branch
	head, err := repo.Head()
	if err == nil && head.Name().IsBranch() {
		branchName := head.Name().Short()
		// Try to get remote for the current branch
		cfg, err := repo.Config()
		if err == nil {
			if b, ok := cfg.Branches[branchName]; ok && b.Remote != "" {
				return b.Remote
			}
		}
	}

	// Fallback to origin
	return DefaultRemote
}

func (r *runner) fetchRemoteShas(repo *Repository, remote string) (map[string]string, error) {
	// Synchronize go-git operations to prevent concurrent packfile access
	goGitMu.Lock()
	rem, err := repo.Remote(remote)
	goGitMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to get remote %s: %w", remote, err)
	}

	// List remote references
	// Synchronize go-git operations to prevent concurrent packfile access
	goGitMu.Lock()
	refs, err := rem.List(&gogit.ListOptions{})
	goGitMu.Unlock()
	if err != nil {
		if errors.Is(err, transport.ErrEmptyRemoteRepository) || errors.Is(err, gogit.NoErrAlreadyUpToDate) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("failed to list remote refs for %s: %w", remote, err)
	}

	remoteShas := make(map[string]string)
	for _, ref := range refs {
		// Only process refs/heads/* (branches)
		if ref.Name().IsBranch() {
			branchName := ref.Name().Short()
			remoteShas[branchName] = ref.Hash().String()
		}
	}

	return remoteShas, nil
}

func (r *runner) getRemoteSha(repo *Repository, remote, branchName string) (string, error) {
	// Synchronize go-git operations to prevent concurrent packfile access
	goGitMu.Lock()
	defer goGitMu.Unlock()

	refName := plumbing.ReferenceName(fmt.Sprintf("refs/remotes/%s/%s", remote, branchName))
	ref, err := repo.Reference(refName, true)
	if err != nil {
		return "", fmt.Errorf("failed to get remote SHA for %s/%s: %w", remote, branchName, err)
	}

	return ref.Hash().String(), nil
}
