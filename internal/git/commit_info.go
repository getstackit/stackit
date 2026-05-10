package git

import (
	"fmt"
	"sync"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/getstackit/stackit/internal/utils"
)

func (r *runner) getCommitDate(repo *Repository, branchName string) (time.Time, error) {
	// Synchronize go-git operations to prevent concurrent packfile access
	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	hash, err := r.resolveRefHashInternal(repo, branchName)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to resolve branch reference: %w", err)
	}

	commit, err := repo.CommitObject(hash)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get commit: %w", err)
	}

	return commit.Author.When, nil
}

func (r *runner) getCommitAuthor(repo *Repository, branchName string) (string, error) {
	// Synchronize go-git operations to prevent concurrent packfile access
	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	hash, err := r.resolveRefHashInternal(repo, branchName)
	if err != nil {
		return "", fmt.Errorf("failed to resolve branch reference: %w", err)
	}

	commit, err := repo.CommitObject(hash)
	if err != nil {
		return "", fmt.Errorf("failed to get commit: %w", err)
	}

	return commit.Author.Name, nil
}

func (r *runner) getRevision(repo *Repository, branchName string) (string, error) {
	// Check revision cache first to avoid go-git mutex contention.
	// The cache is populated by LoadAllBranchRevisions (batch preload)
	// but not on individual misses, to avoid stale entries from external mutations.
	if cached, ok := r.revisionCache.Get(branchName); ok {
		return cached, nil
	}

	// Synchronize go-git operations to prevent concurrent packfile access
	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	hash, err := r.resolveRefHashInternal(repo, branchName)
	if err != nil {
		return "", fmt.Errorf("failed to resolve branch reference: %w", err)
	}

	return hash.String(), nil
}

func (r *runner) getRemoteRevision(repo *Repository, branchName string) (string, error) {
	// Synchronize go-git operations to prevent concurrent packfile access
	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	// Try refs/remotes/origin/branchName
	hash, err := r.resolveRefHashInternal(repo, "origin/"+branchName)
	if err != nil {
		return "", fmt.Errorf("failed to get remote branch reference: %w", err)
	}

	return hash.String(), nil
}

// iterateCommitsNoLock iterates commits from head to base without locking
func iterateCommitsNoLock(repo *Repository, headHash, baseHash plumbing.Hash) ([]*object.Commit, error) {
	var commits []*object.Commit
	currentHash := headHash

	for !currentHash.IsZero() && currentHash != baseHash {
		commit, err := repo.CommitObject(currentHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get commit %s: %w", currentHash, err)
		}

		commits = append(commits, commit)

		if commit.NumParents() == 0 {
			break
		}
		// Follow the first parent for a linear history walk
		currentHash = commit.ParentHashes[0]
	}

	return commits, nil
}

// resolveRefHash resolves a ref (branch name, SHA, or ref path) to a hash
func (r *runner) resolveRefHash(repo *Repository, ref string) (plumbing.Hash, error) {
	// Synchronize go-git operations to prevent concurrent packfile access
	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	return r.resolveRefHashInternal(repo, ref)
}

// resolveRefHashInternal resolves a ref without locking
func (r *runner) resolveRefHashInternal(repo *Repository, ref string) (plumbing.Hash, error) {
	// 1. Try as a full reference name
	if refInfo, err := repo.Reference(plumbing.ReferenceName(ref), true); err == nil {
		return refInfo.Hash(), nil
	}

	// 2. Try as a local branch
	if refInfo, err := repo.Reference(plumbing.ReferenceName("refs/heads/"+ref), true); err == nil {
		return refInfo.Hash(), nil
	}

	// 3. Try as a remote branch
	if refInfo, err := repo.Reference(plumbing.ReferenceName("refs/remotes/origin/"+ref), true); err == nil {
		return refInfo.Hash(), nil
	}

	// 4. Try as a tag
	if refInfo, err := repo.Reference(plumbing.ReferenceName("refs/tags/"+ref), true); err == nil {
		return refInfo.Hash(), nil
	}

	// 5. Try ResolveRevision (handles SHAs, short SHAs, and complex expressions like HEAD~1)
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err == nil {
		return *hash, nil
	}

	return plumbing.ZeroHash, fmt.Errorf("failed to resolve ref %s: reference not found", ref)
}

// LoadAllBranchRevisions populates the revision cache for all local branches
// using one go-git reference iteration. This replaces N individual ref
// resolutions and avoids spawning a git process for the common preload path.
func (r *runner) LoadAllBranchRevisions() error {
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}

	branches, err := repo.Branches()
	if err != nil {
		return fmt.Errorf("failed to list branches: %w", err)
	}
	defer branches.Close()

	return branches.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() {
			r.revisionCache.Put(ref.Name().Short(), ref.Hash().String())
		}
		return nil
	})
}

func (r *runner) batchGetRevisions(repo *Repository, branchNames []string) (map[string]string, []error) {
	results := make(map[string]string)
	var errors []error
	resultsMu := sync.Mutex{}
	errorsMu := sync.Mutex{}

	if len(branchNames) == 0 {
		return results, errors
	}

	utils.Run(branchNames, func(name string) {
		sha, err := r.getRevision(repo, name)
		if err != nil {
			errorsMu.Lock()
			errors = append(errors, fmt.Errorf("failed to get revision for %s: %w", name, err))
			errorsMu.Unlock()
		} else {
			resultsMu.Lock()
			results[name] = sha
			resultsMu.Unlock()
		}
	})

	return results, errors
}
